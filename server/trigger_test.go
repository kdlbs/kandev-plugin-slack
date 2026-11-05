package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kdlbs/kandev/pluginsdk"
)

// --- session fallback: prefix matching ---

func TestHasCommandPrefix(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{name: "exact", text: "!kandev", want: true},
		{name: "with instruction", text: "!kandev fix the login bug", want: true},
		{name: "colon separator", text: "!kandev: fix it", want: true},
		{name: "leading whitespace", text: "   !kandev fix it", want: true},
		{name: "slack quote rendering", text: "> !kandev fix it", want: true},
		{name: "case insensitive", text: "!KanDev fix it", want: true},
		{name: "longer word does not match", text: "!kandevish fix it", want: false},
		{name: "prefix mid-message does not match", text: "please !kandev fix it", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasCommandPrefix(tc.text, "!kandev"); got != tc.want {
				t.Fatalf("hasCommandPrefix(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestStripCommandPrefix(t *testing.T) {
	cases := []struct{ text, want string }{
		{text: "!kandev fix the login bug", want: "fix the login bug"},
		{text: "!kandev: fix it", want: "fix it"},
		{text: "> !kandev fix it", want: "fix it"},
		{text: "!kandev", want: ""},
	}
	for _, tc := range cases {
		if got := stripCommandPrefix(tc.text, "!kandev"); got != tc.want {
			t.Fatalf("stripCommandPrefix(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}

// Slack's `in:` modifiers combine with AND, so two channels in one query match
// nothing.
func TestSearchQueriesOnePerChannel(t *testing.T) {
	cfg := &config{CommandPrefix: "!kandev", Channels: []string{"C1", "C2"}}
	got := searchQueries(cfg, "U1")
	if len(got) != 2 {
		t.Fatalf("got %d queries, want one per channel: %v", len(got), got)
	}
	if !strings.Contains(got[0], "in:<#C1>") || !strings.Contains(got[1], "in:<#C2>") {
		t.Fatalf("queries = %v", got)
	}
}

func TestSearchQueriesWorkspaceWideWhenNoChannels(t *testing.T) {
	got := searchQueries(&config{CommandPrefix: "!kandev"}, "U1")
	if len(got) != 1 || strings.Contains(got[0], "in:") {
		t.Fatalf("queries = %v, want a single unscoped query", got)
	}
}

func TestCollectFiltersAndOrders(t *testing.T) {
	body := `{"ok":true,"messages":{"matches":[
		{"ts":"3.0","text":"!kandev third","channel":{"id":"C1"}},
		{"ts":"1.0","text":"unrelated chatter","channel":{"id":"C1"}},
		{"ts":"2.0","text":"!kandev second","channel":{"id":"C1"}}]}}`
	c, _ := newTestClient(t, body)
	cfg := &config{Mode: authModeSession, CommandPrefix: "!kandev"}
	got, err := collect(context.Background(), c, cfg, "U1", map[string]string{})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d matches, want the 2 prefixed ones: %+v", len(got), got)
	}
	if got[0].TS != "2.0" || got[1].TS != "3.0" {
		t.Fatalf("order = %s, %s; want oldest first", got[0].TS, got[1].TS)
	}
}

func TestCollectHonoursTheWatermark(t *testing.T) {
	body := `{"ok":true,"messages":{"matches":[
		{"ts":"3.0","text":"!kandev newer","channel":{"id":"C1"}},
		{"ts":"1.0","text":"!kandev already done","channel":{"id":"C1"}}]}}`
	c, _ := newTestClient(t, body)
	cfg := &config{Mode: authModeSession, CommandPrefix: "!kandev"}
	got, err := collect(context.Background(), c, cfg, "U1", map[string]string{searchWatermarkKey: "2.0"})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(got) != 1 || got[0].TS != "3.0" {
		t.Fatalf("got %+v, want only the message after the watermark", got)
	}
}

// Without a probed user id the search would be unscoped and match every
// `!kandev` in the workspace, including other people's.
func TestCollectRequiresAnAuthenticatedUser(t *testing.T) {
	c, _ := newTestClient(t, `{"ok":true,"messages":{"matches":[]}}`)
	_, err := collect(context.Background(), c, &config{Mode: authModeSession, CommandPrefix: "!kandev"}, "", map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "authenticated Slack user id") {
		t.Fatalf("collect = %v, want the missing-user-id error", err)
	}
}

// --- supervisor ---

func TestCredentialFingerprintChangesWithTheToken(t *testing.T) {
	a := credentialFingerprint(&config{Mode: authModeApp, AppToken: "xapp-aaaa", BotToken: "xoxb-1111"})
	b := credentialFingerprint(&config{Mode: authModeApp, AppToken: "xapp-bbbb", BotToken: "xoxb-1111"})
	if a == b {
		t.Fatal("rotating the app token must invalidate the fingerprint")
	}
}

// Switching install paths must restart the source even if a token is reused.
func TestCredentialFingerprintChangesWithTheMode(t *testing.T) {
	app := credentialFingerprint(&config{Mode: authModeApp, BotToken: "xoxb-same"})
	session := credentialFingerprint(&config{Mode: authModeSession, BotToken: "xoxb-same"})
	if app == session {
		t.Fatal("a mode change must invalidate the fingerprint")
	}
}

// The fingerprint is compared on every tick and can reach a log; it must not
// be reversible into the credential.
func TestCredentialFingerprintDoesNotEmbedTheSecret(t *testing.T) {
	token := "xapp-super-secret-value"
	got := credentialFingerprint(&config{Mode: authModeApp, AppToken: token})
	if strings.Contains(got, token) || strings.Contains(got, "secret") {
		t.Fatalf("fingerprint %q leaks the token", got)
	}
}

// A queued request already covers the caller's intent, so repeated presses of
// "Scan now" must not block the webhook handler on a busy loop.
func TestScanNowIsNonBlocking(t *testing.T) {
	s := newSupervisor(func() pluginsdk.Host { return nil })
	done := make(chan struct{})
	go func() {
		for range 5 {
			s.ScanNow()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ScanNow blocked")
	}
}

// A nil Host is the normal state between subprocess start and broker
// injection; a tick in that window must be a no-op, not a panic.
func TestTickToleratesAnUninjectedHost(t *testing.T) {
	newSupervisor(func() pluginsdk.Host { return nil }).tick(context.Background(), true)
}

func TestFirstLineTruncates(t *testing.T) {
	if got := firstLine("one\ntwo"); got != "one" {
		t.Fatalf("firstLine = %q, want the first line only", got)
	}
	if got := firstLine(strings.Repeat("x", 200)); len(got) > 130 || !strings.HasSuffix(got, "…") {
		t.Fatalf("firstLine did not truncate: len=%d", len(got))
	}
}
