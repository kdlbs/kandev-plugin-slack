package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kdlbs/kandev/pluginsdk"
)

// The in-tree integration handed the agent Kandev's MCP tools and let it call
// create_task itself. Host.InvokeUtilityAgent is a one-shot completion with
// no tool loop, so triage is inverted here: the plugin reads the topology up
// front, asks for a single JSON decision, and performs the write itself
// through Host.Tasks().Create. That also means a malformed answer fails
// visibly at parse time instead of half-creating something.

// topologyPageSize bounds each Host list call. Well past any realistic
// install, and the agent's context is the real constraint on how much
// topology is worth sending.
const topologyPageSize = 100

// maxWorkflowsForSteps caps how many workflows get their columns expanded.
// Steps are only needed to name a starting column, and listing them costs one
// call per workflow.
const maxWorkflowsForSteps = 12

// workspaceTopology is the Kandev structure offered to the triage agent.
type workspaceTopology struct {
	ID           string             `json:"workspace_id"`
	Name         string             `json:"workspace_name"`
	Workflows    []workflowTopology `json:"workflows"`
	Repositories []string           `json:"repositories,omitempty"`
}

type workflowTopology struct {
	ID    string   `json:"workflow_id"`
	Name  string   `json:"workflow_name"`
	Steps []string `json:"columns,omitempty"`
	// StepIDs is parallel to Steps. It is sent separately so the agent picks a
	// column by name and the plugin resolves the id, rather than the agent
	// echoing an opaque id it may mangle.
	StepIDs []string `json:"-"`
}

// readTopology collects the workspaces, workflows, columns and repositories
// the agent may choose between.
func readTopology(ctx context.Context, host pluginsdk.Host) ([]workspaceTopology, error) {
	workspaces, _, err := host.Workspaces().List(ctx, pluginsdk.Page{Limit: topologyPageSize})
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	if len(workspaces) == 0 {
		return nil, errors.New("no Kandev workspaces exist yet")
	}
	out := make([]workspaceTopology, 0, len(workspaces))
	expanded := 0
	for _, ws := range workspaces {
		entry := workspaceTopology{ID: ws.ID, Name: ws.Name}
		workflows, _, err := host.Workflows().List(ctx, ws.ID, pluginsdk.Page{Limit: topologyPageSize})
		if err != nil {
			return nil, fmt.Errorf("list workflows for %s: %w", ws.Name, err)
		}
		for _, wf := range workflows {
			flow := workflowTopology{ID: wf.ID, Name: wf.Name}
			if expanded < maxWorkflowsForSteps {
				expanded++
				if steps, err := host.Workflows().ListSteps(ctx, wf.ID); err == nil {
					for _, s := range steps {
						flow.Steps = append(flow.Steps, s.Name)
						flow.StepIDs = append(flow.StepIDs, s.ID)
					}
				}
			}
			entry.Workflows = append(entry.Workflows, flow)
		}
		if repos, _, err := host.Repositories().List(ctx, ws.ID, pluginsdk.Page{Limit: topologyPageSize}); err == nil {
			for _, r := range repos {
				entry.Repositories = append(entry.Repositories, r.Name)
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

// triageDecision is the JSON contract the agent answers with.
type triageDecision struct {
	WorkspaceID string `json:"workspace_id"`
	WorkflowID  string `json:"workflow_id"`
	Column      string `json:"column"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Reply       string `json:"reply"`
}

const triageInstructions = `You are a Kandev triage assistant. A user sent a request from Slack: an instruction plus the surrounding thread for context. Decide where in Kandev the work belongs and write the task.

Reply with a single JSON object and nothing else — no prose, no code fence. Use exactly these keys:

{
  "workspace_id": "id of the workspace from the list below",
  "workflow_id":  "id of a workflow inside that workspace",
  "column":       "name of the starting column, exactly as listed, or \"\" for the first one",
  "title":        "short imperative task title",
  "description":  "everything the future agent needs: the request, the relevant thread context, and the Slack link",
  "reply":        "one or two sentences for the human, posted back into the Slack thread"
}

Rules:
- workspace_id and workflow_id must be copied verbatim from the topology below. Never invent an id.
- Pick the workspace whose repositories and workflow names best match what the request is about. When nothing matches, use the first workspace.
- The description is read by an agent with no access to Slack, so inline the context rather than pointing at the link.
- The reply is posted in-thread with no chance for the user to answer back, so do not ask follow-up questions. If the request is ambiguous, choose the most likely reading and say which one you chose.`

// buildTriagePrompt assembles the single completion sent to the utility agent.
func buildTriagePrompt(topology []workspaceTopology, trigger message, instruction, permalink string, thread []message) (string, error) {
	encoded, err := json.MarshalIndent(topology, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode topology: %w", err)
	}
	var b strings.Builder
	b.WriteString(triageInstructions)
	b.WriteString("\n\n--- Kandev topology ---\n")
	b.Write(encoded)
	b.WriteString("\n\n--- Slack request ---\n")
	if permalink != "" {
		b.WriteString("Thread: ")
		b.WriteString(permalink)
		b.WriteString("\n")
	}
	b.WriteString("From ")
	b.WriteString(trigger.sender())
	b.WriteString(":\n")
	b.WriteString(instruction)
	b.WriteString("\n")
	if formatted := formatThread(thread); formatted != "" {
		b.WriteString("\nThread context:\n")
		b.WriteString(formatted)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func formatThread(thread []message) string {
	if len(thread) == 0 {
		return ""
	}
	var b strings.Builder
	for _, m := range thread {
		who := m.UserName
		if who == "" {
			who = m.UserID
		}
		if who == "" {
			who = "(unknown)"
		}
		fmt.Fprintf(&b, "%s (%s): %s\n", who, m.TS, strings.TrimSpace(m.Text))
	}
	return strings.TrimRight(b.String(), "\n")
}

// parseDecision extracts the JSON object from the agent's response. Models
// wrap JSON in prose or a fence often enough that recovering the outermost
// object is worth doing before failing the message and stalling the
// watermark.
func parseDecision(response string) (*triageDecision, error) {
	raw := strings.TrimSpace(response)
	if raw == "" {
		return nil, errors.New("triage agent returned an empty response")
	}
	candidate := stripCodeFence(raw)
	// Trim to the outermost object whenever both braces are present — prose
	// can precede the JSON, follow it, or both, and a decision that only
	// handled a leading preamble would still fail on "…}\nHope that helps."
	if start := strings.Index(candidate, "{"); start >= 0 {
		if end := strings.LastIndex(candidate, "}"); end > start {
			candidate = candidate[start : end+1]
		}
	}
	var decision triageDecision
	if err := json.Unmarshal([]byte(candidate), &decision); err != nil {
		return nil, fmt.Errorf("triage agent did not return JSON: %w", err)
	}
	decision.Title = strings.TrimSpace(decision.Title)
	if decision.Title == "" {
		return nil, errors.New("triage agent returned no task title")
	}
	return &decision, nil
}

// stripCodeFence removes a leading ```/```json fence and its closing pair.
func stripCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:]
	}
	if end := strings.LastIndex(s, "```"); end >= 0 {
		s = s[:end]
	}
	return strings.TrimSpace(s)
}

// resolve validates the agent's choice against the topology and fills in the
// step id. A hallucinated workspace or workflow falls back to the first real
// one rather than failing the message: landing the task somewhere the user
// can see beats dropping their request on the floor.
func (d *triageDecision) resolve(topology []workspaceTopology) (workspaceID, workflowID string, stepID *string) {
	if len(topology) == 0 {
		return "", "", nil
	}
	ws := findWorkspace(topology, d.WorkspaceID)
	if ws == nil {
		ws = &topology[0]
	}
	if len(ws.Workflows) == 0 {
		return ws.ID, "", nil
	}
	flow := findWorkflow(ws.Workflows, d.WorkflowID)
	if flow == nil {
		flow = &ws.Workflows[0]
	}
	return ws.ID, flow.ID, flow.stepIDByName(d.Column)
}

func findWorkspace(topology []workspaceTopology, id string) *workspaceTopology {
	for i := range topology {
		if topology[i].ID == id {
			return &topology[i]
		}
	}
	return nil
}

func findWorkflow(workflows []workflowTopology, id string) *workflowTopology {
	for i := range workflows {
		if workflows[i].ID == id {
			return &workflows[i]
		}
	}
	return nil
}

// stepIDByName maps a column name back to its id. An empty or unknown name
// returns nil so the host applies the workflow's own default column.
func (w *workflowTopology) stepIDByName(name string) *string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	for i, step := range w.Steps {
		if strings.EqualFold(step, name) && i < len(w.StepIDs) {
			id := w.StepIDs[i]
			return &id
		}
	}
	return nil
}

// buildDescription appends the Slack provenance the agent was not asked to
// format itself, so every task links back to the conversation even when the
// model omits it.
func buildDescription(decision *triageDecision, trigger message, permalink string) string {
	body := strings.TrimSpace(decision.Description)
	var b strings.Builder
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	b.WriteString("---\nRequested by ")
	b.WriteString(trigger.sender())
	b.WriteString(" in Slack")
	if permalink != "" {
		b.WriteString(": ")
		b.WriteString(permalink)
	}
	return b.String()
}
