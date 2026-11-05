package main

import (
	"context"
	"time"

	"github.com/kdlbs/kandev/pluginsdk"
)

// Host state is the durable half of the plugin: the watermarks that stop a
// restart from re-triaging every open request, and the last probe result the
// UI renders. Both are small JSON objects, which is exactly what Host state
// is for — see the storage matrix in the authoring guide.
const (
	stateScope     = "instance"
	stateWatermark = "watermarks"
	stateStatus    = "status"

	// searchWatermarkKey is the pseudo-channel the search-based modes record
	// their watermark under. Search results span channels, so there is one
	// watermark for the whole workspace rather than one per channel.
	searchWatermarkKey = "__search__"
)

// maxRecentEntries bounds the activity list shown on the plugin page. Host
// state has no pruning of its own, so the writer has to keep it finite.
const maxRecentEntries = 20

// status is the plugin's externally visible health, persisted so the UI can
// render it immediately after a restart instead of waiting for a probe.
type status struct {
	Configured bool   `json:"configured"`
	Mode       string `json:"mode,omitempty"`
	ModeLabel  string `json:"modeLabel,omitempty"`
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	TeamName   string `json:"teamName,omitempty"`
	TeamID     string `json:"teamId,omitempty"`
	UserID     string `json:"userId,omitempty"`
	UserName   string `json:"userName,omitempty"`
	CheckedAt  string `json:"checkedAt,omitempty"`
	ScannedAt  string `json:"scannedAt,omitempty"`
	Triaged    int    `json:"triaged"`
	Recent     []any  `json:"recent,omitempty"`
}

// recentEntry is one line of the activity feed.
type recentEntry struct {
	At        string `json:"at"`
	Text      string `json:"text"`
	TaskTitle string `json:"taskTitle,omitempty"`
	TaskID    string `json:"taskId,omitempty"`
	Permalink string `json:"permalink,omitempty"`
	Error     string `json:"error,omitempty"`
}

// readStatus loads the persisted status. A missing or unreadable entry
// degrades to the zero value: status is a display concern, and failing the
// poll loop over it would be worse than showing "unknown".
func readStatus(ctx context.Context, host pluginsdk.Host) status {
	value, found, err := host.GetState(ctx, stateScope, "", stateStatus)
	if err != nil || !found {
		return status{}
	}
	return statusFromMap(value)
}

func statusFromMap(value map[string]any) status {
	s := status{
		Configured: boolField(value, "configured"),
		Mode:       stringField(value, "mode"),
		ModeLabel:  stringField(value, "modeLabel"),
		OK:         boolField(value, "ok"),
		Error:      stringField(value, "error"),
		TeamName:   stringField(value, "teamName"),
		TeamID:     stringField(value, "teamId"),
		UserID:     stringField(value, "userId"),
		UserName:   stringField(value, "userName"),
		CheckedAt:  stringField(value, "checkedAt"),
		ScannedAt:  stringField(value, "scannedAt"),
	}
	if n, ok := value["triaged"].(float64); ok {
		s.Triaged = int(n)
	}
	if list, ok := value["recent"].([]any); ok {
		s.Recent = list
	}
	return s
}

func (s status) toMap() map[string]any {
	return map[string]any{
		"configured": s.Configured,
		"mode":       s.Mode,
		"modeLabel":  s.ModeLabel,
		"ok":         s.OK,
		"error":      s.Error,
		"teamName":   s.TeamName,
		"teamId":     s.TeamID,
		"userId":     s.UserID,
		"userName":   s.UserName,
		"checkedAt":  s.CheckedAt,
		"scannedAt":  s.ScannedAt,
		"triaged":    s.Triaged,
		"recent":     s.Recent,
	}
}

func writeStatus(ctx context.Context, host pluginsdk.Host, s status) error {
	return host.SetState(ctx, stateScope, "", stateStatus, s.toMap())
}

// note prepends an activity entry, trimming the tail to maxRecentEntries.
func (s *status) note(entry recentEntry) {
	item := map[string]any{"at": entry.At, "text": entry.Text}
	if entry.TaskTitle != "" {
		item["taskTitle"] = entry.TaskTitle
	}
	if entry.TaskID != "" {
		item["taskId"] = entry.TaskID
	}
	if entry.Permalink != "" {
		item["permalink"] = entry.Permalink
	}
	if entry.Error != "" {
		item["error"] = entry.Error
	}
	s.Recent = append([]any{item}, s.Recent...)
	if len(s.Recent) > maxRecentEntries {
		s.Recent = s.Recent[:maxRecentEntries]
	}
}

// readWatermarks returns the last processed Slack timestamp per channel.
func readWatermarks(ctx context.Context, host pluginsdk.Host) map[string]string {
	out := map[string]string{}
	value, found, err := host.GetState(ctx, stateScope, "", stateWatermark)
	if err != nil || !found {
		return out
	}
	for k, v := range value {
		if ts, ok := v.(string); ok {
			out[k] = ts
		}
	}
	return out
}

func writeWatermarks(ctx context.Context, host pluginsdk.Host, marks map[string]string) error {
	value := make(map[string]any, len(marks))
	for k, v := range marks {
		value[k] = v
	}
	return host.SetState(ctx, stateScope, "", stateWatermark, value)
}

func stringField(value map[string]any, key string) string {
	s, _ := value[key].(string)
	return s
}

func boolField(value map[string]any, key string) bool {
	b, _ := value[key].(bool)
	return b
}

// nowRFC3339 is the single timestamp format written into state, so the UI
// bundle can parse every field the same way.
func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }
