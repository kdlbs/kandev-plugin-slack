package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/kandev/kandev/pkg/pluginsdk"
)

const notificationTool = "notify_user"

var (
	slackUserID     = regexp.MustCompile(`^[UW][A-Z0-9]{8,31}$`)
	slackDMID       = regexp.MustCompile(`^D[A-Z0-9]{8,31}$`)
	slackMessageTS  = regexp.MustCompile(`^[0-9]{1,20}\.[0-9]{6}$`)
	notificationKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
)

var _ pluginsdk.AgentToolPlugin = (*slackPlugin)(nil)

// InvokeAgentTool receives identity only through the host-verified context.
// Task/workspace/session identifiers are deliberately absent from arguments.
func (p *slackPlugin) InvokeAgentTool(ctx context.Context, req *pluginsdk.AgentToolRequest) (*pluginsdk.AgentToolResult, error) {
	if req == nil || req.Name != notificationTool {
		return notificationResult("", "rejected", "unknown_tool", "Unknown notification tool.", "", "", false, 0), nil
	}
	c := req.Context
	if c.TaskID == "" || c.SessionID == "" || c.WorkspaceID == "" || (c.Surface != "kanban-task" && c.Surface != "office-task") {
		return notificationResult("", "rejected", "missing_context", "A host-verified task session and workspace are required.", "", "", false, 0), nil
	}
	recipient, message, key, valid := notificationArguments(req.Arguments)
	if !valid {
		return notificationResult("", "rejected", "invalid_arguments", "Use one Slack user ID, 1–4000 bytes of plain text without mentions or angle brackets, and a 1–128 character idempotency key.", "", "", false, 0), nil
	}
	host := p.Host()
	if host == nil {
		return notificationResult(recipient, "rejected", "host_unavailable", "Plugin host is unavailable; no message was sent.", "", "", false, 0), nil
	}
	p.notifyOnce.Do(func() { p.notifyGate = make(chan struct{}, 1) })
	select {
	case p.notifyGate <- struct{}{}:
		defer func() { <-p.notifyGate }()
	case <-ctx.Done():
		return notificationResult(recipient, "rejected", "cancelled", "Cancelled while waiting; this call did not send.", "", "", false, 0), nil
	}
	// Hash a tuple, rather than concatenating ambiguous user-controlled strings.
	tuple, _ := json.Marshal([]string{c.WorkspaceID, recipient, key})
	stateKey := fmt.Sprintf("notify.v1.%x", sha256.Sum256(tuple))
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(message)))
	record, found, err := host.GetState(ctx, "workspace", c.WorkspaceID, stateKey)
	if err != nil {
		return notificationResult(recipient, "rejected", "state_unavailable", "Cannot read durable notification state; no message was sent. Check the state capability.", "", "", false, 0), nil
	}
	if found {
		if stringField(record, "digest") != digest {
			return notificationResult(recipient, "rejected", "key_conflict", "This key is already bound to different content. No message was sent.", "", "", false, 0), nil
		}
		status := stringField(record, "status")
		switch status {
		case "delivered", "failed", "uncertain":
			result, ok := record["result"].(map[string]any)
			if ok && stringField(result, "recipient") == recipient && stringField(result, "status") == status {
				copy := make(map[string]any, len(result))
				for k, v := range result {
					copy[k] = v
				}
				copy["duplicate"] = true
				return notificationToolResult(copy), nil
			}
		}
		// Pending includes a process crash between Slack accepting a message and
		// saving its receipt. Never turn it into an automatic retry.
		return notificationResult(recipient, "uncertain", "unconfirmed_previous_attempt", "A previous attempt has no confirmed receipt. Do not retry with a new key until an operator checks Slack.", "", "", true, 0), nil
	}
	raw, err := host.GetConfig(ctx)
	if err != nil {
		return notificationResult(recipient, "rejected", "config_unavailable", "Cannot read plugin configuration; no message was sent.", "", "", false, 0), nil
	}
	token := strings.TrimSpace(configString(raw, "bot_token"))
	if !strings.HasPrefix(token, botTokenPrefix) || len(token) <= len(botTokenPrefix) {
		return notificationResult(recipient, "rejected", "bot_not_configured", "Configure the existing xoxb- bot token in Settings > Plugins > Slack. Browser-session credentials cannot deliver notifications.", "", "", false, 0), nil
	}
	record = map[string]any{"digest": digest, "status": "pending", "created_at": nowRFC3339()}
	if err := host.SetState(ctx, "workspace", c.WorkspaceID, stateKey, record); err != nil {
		return notificationResult(recipient, "rejected", "state_unavailable", "Cannot reserve durable notification state; no message was sent. A possibly saved reservation will block repeats.", "", "", false, 0), nil
	}
	// Leave time to persist the outcome within the host's 30-second deadline.
	sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	client := newClient(token, "")
	client.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	result := client.deliverNotification(sendCtx, recipient, message)
	cancel()
	record["status"] = stringField(result.StructuredContent, "status")
	record["result"] = result.StructuredContent
	// Cancellation after a send must not prevent saving its receipt. A failed
	// save leaves the durable pending reservation intact and blocks any resend.
	saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer saveCancel()
	if err := host.SetState(saveCtx, "workspace", c.WorkspaceID, stateKey, record); err != nil {
		return notificationResult(recipient, "uncertain", "receipt_not_saved", "The delivery receipt could not be saved. Do not retry with a new key until an operator checks Slack.", stringField(result.StructuredContent, "channel"), stringField(result.StructuredContent, "message_ts"), false, 0), nil
	}
	return result, nil
}

func notificationArguments(args map[string]any) (recipient, message, key string, valid bool) {
	if len(args) != 3 {
		return
	}
	recipient, _ = args["recipient"].(string)
	message, _ = args["message"].(string)
	key, _ = args["idempotency_key"].(string)
	if !slackUserID.MatchString(recipient) || !notificationKey.MatchString(key) || !utf8.ValidString(message) || len(message) > 4000 || strings.TrimSpace(message) == "" {
		return
	}
	// Plain URLs work. Reject Slack markup and every @ mention, including
	// broadcast spellings; mrkdwn/automatic parsing are also disabled below.
	if strings.ContainsAny(message, "<>@") {
		return
	}
	for _, r := range message {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return
		}
	}
	valid = true
	return
}

func notificationResult(recipient, status, code, detail, channel, ts string, duplicate bool, retryAfter int) *pluginsdk.AgentToolResult {
	result := map[string]any{"recipient": recipient, "status": status, "code": code, "detail": detail, "duplicate": duplicate}
	if channel != "" {
		result["channel"] = channel
	}
	if ts != "" {
		result["message_ts"] = ts
	}
	if retryAfter > 0 {
		result["retry_after_seconds"] = retryAfter
	}
	return notificationToolResult(result)
}

// Kandev 0.88.0 (and current hosts) drops structuredContent on error results.
// JSON fallback text preserves every field without disguising IsError.
func notificationToolResult(result map[string]any) *pluginsdk.AgentToolResult {
	text, _ := json.Marshal(result)
	return &pluginsdk.AgentToolResult{Text: string(text), StructuredContent: result, IsError: stringField(result, "status") != "delivered"}
}

func (c *client) deliverNotification(ctx context.Context, recipient, text string) *pluginsdk.AgentToolResult {
	var opened struct {
		Channel struct {
			ID string `json:"id"`
		} `json:"channel"`
	}
	if err := c.post(ctx, "conversations.open", url.Values{"users": {recipient}}, &opened); err != nil {
		return notificationFailure(recipient, "", err, false)
	}
	channel := opened.Channel.ID
	if !slackDMID.MatchString(channel) {
		return notificationResult(recipient, "failed", "invalid_dm_response", "Slack did not return a direct-message channel; no message was sent.", "", "", false, 0)
	}
	var posted struct {
		Channel string `json:"channel"`
		TS      string `json:"ts"`
	}
	params := url.Values{"channel": {channel}, "text": {text}, "mrkdwn": {"false"}, "parse": {"none"}, "link_names": {"false"}, "unfurl_links": {"false"}, "unfurl_media": {"false"}}
	if err := c.post(ctx, "chat.postMessage", params, &posted); err != nil {
		return notificationFailure(recipient, channel, err, true)
	}
	if posted.Channel != channel || !slackMessageTS.MatchString(posted.TS) {
		return notificationResult(recipient, "uncertain", "invalid_receipt", "Slack returned an incomplete delivery receipt. Do not resend until an operator checks Slack.", channel, "", false, 0)
	}
	return notificationResult(recipient, "delivered", "delivered", "Slack confirmed the direct message was delivered.", channel, posted.TS, false, 0)
}

// Only allowlisted Slack rejection codes are safe to return and count as
// confirmed failures. Raw response bodies and transport errors may contain
// credentials or reflect message text; never expose or log them here.
func notificationFailure(recipient, channel string, err error, sending bool) *pluginsdk.AgentToolResult {
	status, code, detail, retry := "failed", "slack_unavailable", "Slack could not open the DM; no message was sent.", 0
	if sending {
		status, code, detail = "uncertain", "unconfirmed_send", "Slack did not confirm delivery. Do not resend until an operator checks Slack."
	}
	var api *apiError
	if errors.As(err, &api) {
		if api.StatusCode == http.StatusTooManyRequests {
			status, code, detail, retry = "failed", "rate_limited", "Slack rejected the request due to rate limits. Wait for Retry-After before an intentional new attempt with a new key.", api.RetryAfter
		} else if api.StatusCode == http.StatusOK {
			switch api.Message {
			case "invalid_auth", "not_authed", "token_revoked", "token_expired", "account_inactive":
				status, code, detail = "failed", "invalid_credentials", "Slack rejected the bot credentials. Reconnect the Slack app; no message was delivered."
			case "missing_scope", "no_permission", "not_allowed_token_type":
				status, code, detail = "failed", "missing_permissions", "Slack rejected delivery permissions. Grant im:write and chat:write to the bot and reinstall the Slack app."
			case "user_not_found", "user_not_visible", "user_disabled", "cannot_dm_bot", "cannot_dm_self", "channel_not_found", "is_archived", "restricted_action", "access_denied", "ekm_access_denied", "invalid_user_combination":
				status, code, detail = "failed", "recipient_unreachable", "Slack rejected this recipient or DM. Check that the user is active and reachable by this bot."
			case "ratelimited":
				status, code, detail = "failed", "rate_limited", "Slack rejected the request due to rate limits. Wait before an intentional new attempt with a new key."
			}
		}
	}
	return notificationResult(recipient, status, code, detail, channel, "", false, retry)
}
