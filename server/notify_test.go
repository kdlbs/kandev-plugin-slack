package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kandev/kandev/pkg/pluginsdk"
)

// Persist through JSON on disk so restart tests also exercise the protobuf
// Struct-compatible value types rather than sharing an in-memory receipt.
type notificationHost struct {
	pluginsdk.Host
	config             map[string]any
	dir                string
	readErr, configErr bool
	failWrite          int
	writes             int
}

func (h *notificationHost) GetConfig(context.Context) (map[string]any, error) {
	if h.configErr {
		return nil, errors.New("secret config diagnostic xoxb-secret")
	}
	return h.config, nil
}
func (h *notificationHost) GetState(_ context.Context, scope, scopeID, key string) (map[string]any, bool, error) {
	if h.readErr {
		return nil, false, errors.New("permission denied xoxb-secret")
	}
	b, err := os.ReadFile(filepath.Join(h.dir, scope+"-"+scopeID+"-"+key))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var value map[string]any
	err = json.Unmarshal(b, &value)
	return value, true, err
}
func (h *notificationHost) SetState(ctx context.Context, scope, scopeID, key string, value map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h.writes++
	if h.writes == h.failWrite {
		return errors.New("permission denied xoxb-secret")
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(h.dir, scope+"-"+scopeID+"-"+key), b, 0600)
}
func notificationFixture(t *testing.T, handler http.HandlerFunc) (*slackPlugin, *notificationHost) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	old := slackAPIBase
	slackAPIBase = server.URL
	t.Cleanup(func() { slackAPIBase = old })
	host := &notificationHost{config: map[string]any{"bot_token": "xoxb-secret"}, dir: t.TempDir()}
	plugin := &slackPlugin{}
	plugin.UnimplementedPlugin.SetHost(host) // no inbound supervisor in these tests
	return plugin, host
}
func notificationRequest() *pluginsdk.AgentToolRequest {
	return &pluginsdk.AgentToolRequest{Name: notificationTool, Context: pluginsdk.AgentToolContext{TaskID: "task-1", SessionID: "session-1", WorkspaceID: "workspace-1", Surface: "office-task"}, Arguments: map[string]any{"recipient": "U12345678", "message": "Please review https://kandev.example/tasks/123", "idempotency_key": "task-123:waiting-1"}}
}
func invokeNotification(t *testing.T, p *slackPlugin, req *pluginsdk.AgentToolRequest) map[string]any {
	t.Helper()
	result, err := p.InvokeAgentTool(context.Background(), req)
	if err != nil || result == nil {
		t.Fatalf("invoke: %v, %v", result, err)
	}
	if result.IsError != (stringField(result.StructuredContent, "status") != "delivered") {
		t.Fatal("incorrect IsError")
	}
	return result.StructuredContent
}
func assertNotification(t *testing.T, result map[string]any, status, code string) {
	t.Helper()
	if result["status"] != status || result["code"] != code {
		t.Fatalf("result = %#v", result)
	}
}
func successNotification(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/conversations.open" {
		_, _ = io.WriteString(w, `{"ok":true,"channel":{"id":"D12345678"}}`)
		return
	}
	_, _ = io.WriteString(w, `{"ok":true,"channel":"D12345678","ts":"1234567890.123456"}`)
}
func TestNotificationValidation(t *testing.T) {
	cases := []struct {
		name, field string
		value       any
	}{
		{"channel", "recipient", "C12345678"}, {"group", "recipient", "G12345678"}, {"multiple", "recipient", "U12345678,U87654321"}, {"short", "recipient", "U1"},
		{"empty", "message", " "}, {"oversized", "message", strings.Repeat("a", 4001)}, {"bytes", "message", strings.Repeat("é", 2001)}, {"broadcast", "message", "<!channel>"}, {"here", "message", "@here"}, {"user mention", "message", "<@U12345678>"}, {"markup", "message", "<https://example.com|link>"}, {"control", "message", "x\x00y"}, {"invalid utf8", "message", string([]byte{255})},
		{"empty key", "idempotency_key", ""}, {"long key", "idempotency_key", strings.Repeat("a", 129)}, {"key whitespace", "idempotency_key", "a b"}, {"wrong type", "message", 1}, {"spoof", "workspace_id", "workspace-2"},
	}
	p, h := notificationFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid request reached Slack") })
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := notificationRequest()
			req.Arguments[tc.field] = tc.value
			assertNotification(t, invokeNotification(t, p, req), "rejected", "invalid_arguments")
		})
	}
	for _, surface := range []string{"external", "configuration", ""} {
		req := notificationRequest()
		req.Context.Surface = surface
		assertNotification(t, invokeNotification(t, p, req), "rejected", "missing_context")
	}
	for _, field := range []string{"task", "session", "workspace"} {
		req := notificationRequest()
		switch field {
		case "task":
			req.Context.TaskID = ""
		case "session":
			req.Context.SessionID = ""
		case "workspace":
			req.Context.WorkspaceID = ""
		}
		assertNotification(t, invokeNotification(t, p, req), "rejected", "missing_context")
	}
	assertNotification(t, invokeNotification(t, p, nil), "rejected", "unknown_tool")
	if h.writes != 0 {
		t.Fatal("invalid input reserved state")
	}
}
func TestNotificationConcurrentRestartAndScope(t *testing.T) {
	var sends atomic.Int32
	p, h := notificationFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer xoxb-secret" || r.Header.Get("Cookie") != "" {
			t.Error("incorrect credentials")
		}
		_ = r.ParseForm()
		if r.URL.Path == "/conversations.open" {
			if len(r.Form["users"]) != 1 || !slackUserID.MatchString(r.Form.Get("users")) {
				t.Error("wrong recipient")
			}
		} else {
			sends.Add(1)
			for k, v := range map[string]string{"channel": "D12345678", "mrkdwn": "false", "parse": "none", "link_names": "false", "unfurl_links": "false", "unfurl_media": "false"} {
				if r.Form.Get(k) != v {
					t.Errorf("%s=%s", k, r.Form.Get(k))
				}
			}
		}
		successNotification(w, r)
	})
	var wg sync.WaitGroup
	var originals atomic.Int32
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			result := invokeNotification(t, p, notificationRequest())
			assertNotification(t, result, "delivered", "delivered")
			if result["duplicate"] == false {
				originals.Add(1)
			}
		})
	}
	wg.Wait()
	if sends.Load() != 1 || originals.Load() != 1 {
		t.Fatalf("sends=%d originals=%d", sends.Load(), originals.Load())
	}
	// A fresh plugin and Host instance read the same durable directory.
	restarted := &slackPlugin{}
	restarted.UnimplementedPlugin.SetHost(&notificationHost{dir: h.dir})
	result := invokeNotification(t, restarted, notificationRequest())
	assertNotification(t, result, "delivered", "delivered")
	if result["duplicate"] != true || result["channel"] != "D12345678" || result["message_ts"] != "1234567890.123456" {
		t.Fatalf("receipt=%v", result)
	}
	req := notificationRequest()
	req.Arguments["message"] = "different"
	assertNotification(t, invokeNotification(t, restarted, req), "rejected", "key_conflict")
	req = notificationRequest()
	req.Context.TaskID = "other-task"
	req.Context.SessionID = "other-session"
	assertNotification(t, invokeNotification(t, restarted, req), "delivered", "delivered")
	req = notificationRequest()
	req.Context.WorkspaceID = "workspace-2"
	assertNotification(t, invokeNotification(t, p, req), "delivered", "delivered")
	req = notificationRequest()
	req.Arguments["recipient"] = "W12345678"
	assertNotification(t, invokeNotification(t, p, req), "delivered", "delivered")
	if sends.Load() != 3 {
		t.Fatalf("scoped sends=%d", sends.Load())
	}
}
func TestNotificationFailures(t *testing.T) {
	cases := []struct {
		name               string
		httpStatus         int
		body, status, code string
	}{
		{"missing scope", 200, `{"ok":false,"error":"missing_scope","needed":"xoxb-secret"}`, "failed", "missing_permissions"},
		{"revoked", 200, `{"ok":false,"error":"token_revoked"}`, "failed", "invalid_credentials"},
		{"disabled", 200, `{"ok":false,"error":"user_disabled"}`, "failed", "recipient_unreachable"},
		{"unreachable", 200, `{"ok":false,"error":"channel_not_found"}`, "failed", "recipient_unreachable"},
		{"rate limit", 429, `xoxb-secret`, "failed", "rate_limited"},
		{"internal error", 200, `{"ok":false,"error":"internal_error"}`, "uncertain", "unconfirmed_send"},
		{"unknown error", 200, `{"ok":false,"error":"xoxb-secret"}`, "uncertain", "unconfirmed_send"},
		{"server error", 503, `xoxb-secret`, "uncertain", "unconfirmed_send"},
		{"bad response", 200, `xoxb-secret`, "uncertain", "unconfirmed_send"},
		{"missing receipt", 200, `{"ok":true}`, "uncertain", "invalid_receipt"},
		{"wrong channel", 200, `{"ok":true,"channel":"C12345678","ts":"1234567890.123456"}`, "uncertain", "invalid_receipt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sends int
			p, h := notificationFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/conversations.open" {
					successNotification(w, r)
					return
				}
				sends++
				w.Header().Set("Retry-After", "37")
				w.WriteHeader(tc.httpStatus)
				_, _ = io.WriteString(w, tc.body)
			})
			result := invokeNotification(t, p, notificationRequest())
			assertNotification(t, result, tc.status, tc.code)
			if tc.httpStatus == 429 && result["retry_after_seconds"] != 37 {
				t.Fatalf("missing retry delay: %v", result)
			}
			p = &slackPlugin{}
			p.UnimplementedPlugin.SetHost(h)
			repeat := invokeNotification(t, p, notificationRequest())
			assertNotification(t, repeat, tc.status, tc.code)
			if repeat["duplicate"] != true || sends != 1 {
				t.Fatalf("repeat=%v sends=%d", repeat, sends)
			}
			encoded, _ := json.Marshal(result)
			if strings.Contains(string(encoded), "xoxb-secret") {
				t.Fatal("credential leaked")
			}
			files, _ := os.ReadDir(h.dir)
			for _, file := range files {
				data, _ := os.ReadFile(filepath.Join(h.dir, file.Name()))
				if strings.Contains(string(data), "xoxb-secret") {
					t.Fatal("credential stored")
				}
			}
		})
	}
}
func TestNotificationStateAndConfigFailures(t *testing.T) {
	for _, which := range []string{"read", "reserve", "receipt", "config", "missing bot"} {
		t.Run(which, func(t *testing.T) {
			var sends int
			p, h := notificationFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/chat.postMessage" {
					sends++
				}
				successNotification(w, r)
			})
			code := "state_unavailable"
			status := "rejected"
			switch which {
			case "read":
				h.readErr = true
			case "reserve":
				h.failWrite = 1
			case "receipt":
				h.failWrite = 2
				code = "receipt_not_saved"
				status = "uncertain"
			case "config":
				h.configErr = true
				code = "config_unavailable"
			case "missing bot":
				h.config = map[string]any{"session_token": "xoxc-secret", "session_cookie": "secret"}
				code = "bot_not_configured"
			}
			result := invokeNotification(t, p, notificationRequest())
			assertNotification(t, result, status, code)
			if strings.Contains(fmt.Sprint(result), "xoxb-secret") {
				t.Fatal("secret leaked")
			}
			if which == "receipt" {
				if sends != 1 {
					t.Fatalf("sends=%d", sends)
				}
				p = &slackPlugin{}
				p.UnimplementedPlugin.SetHost(h)
				assertNotification(t, invokeNotification(t, p, notificationRequest()), "uncertain", "unconfirmed_previous_attempt")
				if sends != 1 {
					t.Fatal("repeated ambiguous send")
				}
			} else if sends != 0 {
				t.Fatal("sent before prerequisites")
			}
		})
	}
}
func TestNotificationTransportFailureAndCancellation(t *testing.T) {
	p, _ := notificationFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/conversations.open" {
			successNotification(w, r)
			return
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	})
	assertNotification(t, invokeNotification(t, p, notificationRequest()), "uncertain", "unconfirmed_send")
	// Waiting for an active invocation is bounded by the caller's context.
	p.notifyGate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := p.InvokeAgentTool(ctx, notificationRequest())
	<-p.notifyGate
	if err != nil {
		t.Fatal(err)
	}
	assertNotification(t, result.StructuredContent, "rejected", "cancelled")
}
func TestNotificationCancellationAfterSendSavesUncertainOutcome(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, host := notificationFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/conversations.open" {
			successNotification(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		cancel()
		<-r.Context().Done()
	})
	result, err := p.InvokeAgentTool(ctx, notificationRequest())
	if err != nil {
		t.Fatal(err)
	}
	assertNotification(t, result.StructuredContent, "uncertain", "unconfirmed_send")
	restarted := &slackPlugin{}
	restarted.UnimplementedPlugin.SetHost(host)
	resultMap := invokeNotification(t, restarted, notificationRequest())
	assertNotification(t, resultMap, "uncertain", "unconfirmed_send")
	if resultMap["duplicate"] != true {
		t.Fatal("cancelled send did not retain its record")
	}
}

type notificationTimeoutTransport struct{}

func (notificationTimeoutTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, "/conversations.open") {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"channel":{"id":"D12345678"}}`)), Header: http.Header{}}, nil
	}
	return nil, context.DeadlineExceeded
}
func TestNotificationTimeout(t *testing.T) {
	c := newClient("xoxb-secret", "")
	c.http.Transport = notificationTimeoutTransport{}
	result := c.deliverNotification(context.Background(), "U12345678", "message")
	assertNotification(t, result.StructuredContent, "uncertain", "unconfirmed_send")
}
func TestNotificationNeverPostsToNonDM(t *testing.T) {
	p, _ := notificationFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/conversations.open" {
			t.Error("sent to non-DM")
		}
		_, _ = io.WriteString(w, `{"ok":true,"channel":{"id":"C12345678"}}`)
	})
	assertNotification(t, invokeNotification(t, p, notificationRequest()), "failed", "invalid_dm_response")
}
