package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/kdlbs/kandev/pluginsdk"
)

// acknowledgeReaction is added to a matched message before the agent runs, so
// the requester sees their message was picked up during the seconds triage
// takes.
const acknowledgeReaction = "eyes"

// inboundRequest is one thing to triage, normalized across the two ways a
// request can arrive: a Socket Mode mention or slash command, or a message the
// session fallback found by polling search. Everything downstream — thread
// fetch, agent call, task creation, reply — works the same either way.
type inboundRequest struct {
	ChannelID string
	TS        string
	ThreadTS  string
	UserID    string
	UserName  string
	// Text is the raw message, kept for the activity feed.
	Text string
	// Instruction is Text with the addressing removed: the mention prefix, the
	// slash command, or the `!kandev` marker.
	Instruction string
	Permalink   string
	// ResponseURL is set for slash commands: a pre-authorized callback that
	// answers the person who ran the command, in channels the bot may not be
	// a member of.
	ResponseURL string
	// Acknowledge is false when there is no message in the channel to react
	// to — a slash command is only visible to the person who ran it.
	Acknowledge bool
}

// message converts the request into the shape the thread formatting and
// description builder expect.
func (r inboundRequest) message() message {
	return message{
		TS:        r.TS,
		ThreadTS:  r.ThreadTS,
		ChannelID: r.ChannelID,
		UserID:    r.UserID,
		UserName:  r.UserName,
		Text:      r.Text,
		Permalink: r.Permalink,
	}
}

// replyThread is the thread a response belongs in. A request that came from a
// real message threads under it; a slash command has no message, so its reply
// starts a new thread.
func (r inboundRequest) replyThread() string {
	if r.ThreadTS != "" {
		return r.ThreadTS
	}
	return r.TS
}

// runner performs triage. It is shared by both sources so a request behaves
// identically however it arrived.
type runner struct {
	host func() pluginsdk.Host

	// seenMu guards seen. Socket Mode can deliver the same envelope twice —
	// Slack retries anything not acknowledged in time, and a reconnect can
	// replay — so the same message must not produce two tasks.
	seenMu sync.Mutex
	seen   map[string]bool
	// seenOrder bounds the dedup set; Host state is not involved because a
	// restart drops the socket's retry window with it.
	seenOrder []string
}

// maxSeenRequests bounds the in-memory dedup set. Slack's retry window is
// seconds, so anything close to this is far past the point of replay.
const maxSeenRequests = 512

func newRunner(host func() pluginsdk.Host) *runner {
	return &runner{host: host, seen: make(map[string]bool)}
}

// requestKey identifies a request across redeliveries and retries.
func requestKey(req inboundRequest) string {
	return req.ChannelID + "/" + req.TS + "/" + req.Instruction
}

// claim reports whether this request is new. A repeat — Slack's retry, or a
// replay after a reconnect — is dropped rather than triaged twice. The claim
// is taken before the work starts, so two in-flight copies cannot both run.
func (r *runner) claim(req inboundRequest) bool {
	key := requestKey(req)
	r.seenMu.Lock()
	defer r.seenMu.Unlock()
	if r.seen[key] {
		return false
	}
	r.seen[key] = true
	r.seenOrder = append(r.seenOrder, key)
	if len(r.seenOrder) > maxSeenRequests {
		delete(r.seen, r.seenOrder[0])
		r.seenOrder = r.seenOrder[1:]
	}
	return true
}

// release drops a claim so a failed request can be retried. Without it the
// claim taken above would make the failure permanent: the session fallback
// re-finds the same message on its next scan and would discard it as a
// duplicate, and a redelivered Socket Mode envelope would meet the same fate.
func (r *runner) release(req inboundRequest) {
	key := requestKey(req)
	r.seenMu.Lock()
	defer r.seenMu.Unlock()
	if !r.seen[key] {
		return
	}
	delete(r.seen, key)
	for i, existing := range r.seenOrder {
		if existing == key {
			r.seenOrder = append(r.seenOrder[:i], r.seenOrder[i+1:]...)
			break
		}
	}
}

// Handle triages one request end to end. The returned error is never sent back
// to Slack — the socket envelope is already acknowledged — but the session
// fallback needs it to decide whether its watermark may advance past this
// message.
func (r *runner) Handle(ctx context.Context, cfg *config, req inboundRequest) error {
	if !r.claim(req) {
		return nil
	}
	host := r.host()
	if host == nil {
		r.release(req)
		return errors.New("plugin host unavailable")
	}
	entry, err := r.triage(ctx, host, cfg, req)
	if err != nil {
		// Give the claim back so the next scan (or redelivery) can retry.
		r.release(req)
		log.Printf("slack: triage failed: %v", err)
		entry = recentEntry{At: nowRFC3339(), Text: firstLine(req.Instruction), Error: err.Error()}
	}
	r.record(ctx, host, entry, err)
	return err
}

// record appends the outcome to the status record the plugin page renders.
func (r *runner) record(ctx context.Context, host pluginsdk.Host, entry recentEntry, err error) {
	st := readStatus(ctx, host)
	if err != nil {
		st.Error = err.Error()
	} else {
		st.Error = ""
		st.Triaged++
	}
	st.note(entry)
	if writeErr := writeStatus(ctx, host, st); writeErr != nil {
		log.Printf("slack: persist status: %v", writeErr)
	}
}

// triage runs the full flow: acknowledge, gather context, ask the agent,
// create the task, reply.
func (r *runner) triage(
	ctx context.Context, host pluginsdk.Host, cfg *config, req inboundRequest,
) (recentEntry, error) {
	token, cookie := cfg.WebCredentials()
	cl := newClient(token, cookie)

	if req.Acknowledge {
		if err := cl.AddReaction(ctx, req.ChannelID, req.TS, acknowledgeReaction); err != nil {
			// A missing reactions:write scope should not block the real work.
			log.Printf("slack: acknowledge reaction failed: %v", err)
		}
	}
	thread, err := cl.ConversationContext(ctx, req.ChannelID, req.ThreadTS, req.TS)
	if err != nil {
		// A channel the app cannot read history for still deserves a task from
		// the instruction alone. Context is a bonus, not a precondition.
		log.Printf("slack: conversation context unavailable: %v", err)
		thread = nil
	}
	permalink := req.Permalink
	if permalink == "" && req.TS != "" {
		permalink, _ = cl.Permalink(ctx, req.ChannelID, req.TS)
		req.Permalink = permalink
	}

	topology, err := readTopology(ctx, host)
	if err != nil {
		return recentEntry{}, err
	}
	prompt, err := buildTriagePrompt(topology, req.message(), req.Instruction, permalink, thread)
	if err != nil {
		return recentEntry{}, err
	}
	response, err := host.InvokeUtilityAgent(ctx, prompt)
	if err != nil {
		return recentEntry{}, fmt.Errorf("triage agent: %w", err)
	}
	decision, err := parseDecision(response)
	if err != nil {
		return recentEntry{}, err
	}
	task, err := createTask(ctx, host, cfg, decision, topology, req.message(), permalink)
	if err != nil {
		return recentEntry{}, err
	}
	r.reply(ctx, cl, req, decision, task)
	return recentEntry{
		At:        nowRFC3339(),
		Text:      firstLine(req.Instruction),
		TaskTitle: task.Title,
		TaskID:    task.ID,
		Permalink: permalink,
	}, nil
}

func createTask(
	ctx context.Context, host pluginsdk.Host, cfg *config,
	decision *triageDecision, topology []workspaceTopology, m message, permalink string,
) (*pluginsdk.Task, error) {
	workspaceID, workflowID, stepID := decision.resolve(topology)
	task, err := host.Tasks().Create(ctx, pluginsdk.CreateTaskInput{
		WorkspaceID:    workspaceID,
		WorkflowID:     workflowID,
		WorkflowStepID: stepID,
		Title:          decision.Title,
		Description:    buildDescription(decision, m, permalink),
		StartAgent:     cfg.StartAgent,
	})
	if err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}
	return task, nil
}

// reply sends the agent's summary back to Slack by whichever route the request
// arrived on. A failure is logged but not returned: the task exists, and
// failing here would re-triage into a duplicate.
func (r *runner) reply(
	ctx context.Context, cl *client, req inboundRequest,
	decision *triageDecision, task *pluginsdk.Task,
) {
	body := strings.TrimSpace(decision.Reply)
	if body == "" {
		body = "Created a Kandev task for this."
	}
	if task != nil {
		if task.Identifier != "" {
			body += fmt.Sprintf("\n\n*%s* — %s", task.Identifier, task.Title)
		} else {
			body += "\n\n*" + task.Title + "*"
		}
	}
	// A slash command was invoked privately and may well be in a channel the
	// bot was never invited to. Answering in-channel would both expose a
	// private action and fail outright on membership, leaving a task created
	// with no feedback at all.
	if req.ResponseURL != "" {
		if err := cl.RespondToCommand(ctx, req.ResponseURL, body); err != nil {
			log.Printf("slack: command response failed: %v", err)
		}
		return
	}
	if err := cl.PostMessage(ctx, req.ChannelID, req.replyThread(), body); err != nil {
		log.Printf("slack: reply failed: %v", err)
	}
}

// errNoUtilityAgent surfaces the one configuration failure the agent call can
// report that the operator can actually fix from the plugin page.
var errNoUtilityAgent = errors.New("no triage agent is selected")

// firstLine truncates a message for the activity feed.
func firstLine(text string) string {
	const maxLen = 120
	line := strings.TrimSpace(text)
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	if len(line) > maxLen {
		return line[:maxLen] + "…"
	}
	return line
}
