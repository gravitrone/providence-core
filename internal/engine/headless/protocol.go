// Package headless defines the CC-compatible NDJSON protocol types and
// Providence extension event types for headless mode communication.
package headless

// --- CC-Compatible Event Types ---

const (
	TypeSystem          = "system"
	TypeAssistant       = "assistant"
	TypeUser            = "user"
	TypeResult          = "result"
	TypeStreamEvent     = "stream_event"
	TypeToolProgress    = "tool_progress"
	TypeRateLimitEvent  = "rate_limit_event"
	TypeControlRequest  = "control_request"
	TypeControlResponse = "control_response"
	TypeKeepAlive       = "keep_alive"
)

// --- Providence Extension Types ---

const (
	TypeHarnessSwitch   = "harness_switch"
	TypeForkSpawn       = "fork_spawn"
	TypeForkMerge       = "fork_merge"
	TypeDashboardUpdate = "dashboard_update"
	TypeCompactEvent    = "compact_event"
)

// --- Subtypes ---

const (
	SubtypeInit                = "init"
	SubtypeSuccess             = "success"
	SubtypeError               = "error_during_execution"
	SubtypeMaxTurns            = "error_max_turns"
	SubtypeSessionStateChanged = "session_state_changed"
	SubtypeAPIRetry            = "api_retry"
	SubtypeCompactBoundary     = "compact_boundary"
	SubtypeStatus              = "status"
)

// SystemInitEvent is sent on startup to announce session capabilities.
type SystemInitEvent struct {
	Type      string   `json:"type"`
	Subtype   string   `json:"subtype"`
	SessionID string   `json:"session_id"`
	Tools     []string `json:"tools"`
	Model     string   `json:"model"`
	Engine    string   `json:"engine"`
}

// ResultEvent is sent on turn completion with outcome and metrics.
type ResultEvent struct {
	Type     string `json:"type"`
	Subtype  string `json:"subtype"`
	Result   string `json:"result"`
	NumTurns int    `json:"num_turns"`
	Duration int64  `json:"duration_ms"`
}

// ControlRequest represents a bidirectional control message between host and
// providence (or vice versa). The Request payload is intentionally untyped to
// allow protocol evolution without schema changes.
type ControlRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Request   any    `json:"request"`
}

// ControlResponse carries the reply to a ControlRequest.
type ControlResponse struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Response  any    `json:"response"`
}

// HarnessSwitchEvent signals that the active engine backend has changed.
type HarnessSwitchEvent struct {
	Type       string `json:"type"`
	FromEngine string `json:"from_engine"`
	ToEngine   string `json:"to_engine"`
	Reason     string `json:"reason,omitempty"`
}

// ForkSpawnEvent signals that a new forked subagent has been created.
type ForkSpawnEvent struct {
	Type    string `json:"type"`
	ForkID  string `json:"fork_id"`
	Engine  string `json:"engine"`
	Model   string `json:"model"`
	Purpose string `json:"purpose,omitempty"`
}

// ForkMergeEvent signals that a forked subagent has completed and merged.
type ForkMergeEvent struct {
	Type   string `json:"type"`
	ForkID string `json:"fork_id"`
	Result string `json:"result"`
}

// DashboardUpdateEvent carries a panel refresh for the split-pane dashboard.
type DashboardUpdateEvent struct {
	Type    string `json:"type"`
	PanelID string `json:"panel_id"`
	Data    any    `json:"data"`
}

// CompactEvent carries lifecycle updates from the compaction pipeline.
type CompactEvent struct {
	Type         string `json:"type"`
	Phase        string `json:"phase"`
	TokensBefore int    `json:"tokens_before,omitempty"`
	TokensAfter  int    `json:"tokens_after,omitempty"`
}

type headlessAssistantMsg struct {
	Content []headlessContentPart `json:"content"`
}

type headlessContentPart struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Input   any    `json:"input,omitempty"`
	Summary string `json:"summary,omitempty"`
}
