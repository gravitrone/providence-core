package overlay

import (
	"encoding/json"
	"time"
)

// --- Protocol Constants ---

// ProtocolVersion is the overlay wire protocol version.
const ProtocolVersion = 1

// Message type constants for Envelope.Type.
const (
	TypeWelcome        = "welcome"
	TypeHello          = "hello"
	TypeContextUpdate  = "context_update"
	TypeUserQuery      = "user_query"
	TypeEmberRequest   = "ember_request"
	TypeSessionEvent   = "session_event"
	TypeAssistantDelta = "assistant_delta"
	TypeEmberState     = "ember_state"
	TypeUIEvent        = "ui_event"
	TypeInterrupt      = "interrupt"
	TypeBye            = "bye"
	TypeGoodbye        = "goodbye"
	TypeContextAck     = "context_ack"
)

// --- Wire Types ---

// Envelope wraps every message in the overlay protocol. NDJSON framing - one
// envelope per line. V is always ProtocolVersion. ID is optional correlation
// tag used by request/response pairs.
type Envelope struct {
	V    int             `json:"v"`
	Type string          `json:"type"`
	ID   string          `json:"id,omitempty"`
	Data json.RawMessage `json:"data"`
}

// Welcome is sent TUI -> Overlay immediately after a successful Hello exchange.
type Welcome struct {
	SessionID   string    `json:"session_id"`
	Engine      string    `json:"engine"`
	Model       string    `json:"model"`
	EmberActive bool      `json:"ember_active"`
	CWD         string    `json:"cwd"`
	Timestamp   time.Time `json:"timestamp"`
}

// Hello is sent Overlay -> TUI as the first message on connect to identify
// the client and declare its capability flags.
type Hello struct {
	ClientVersion string   `json:"client_version"`
	Capabilities  []string `json:"capabilities"` // e.g. ["scstream","whisperkit","porcupine"]
	PID           int      `json:"pid"`
}

// ContextUpdate is sent Overlay -> TUI when the overlay observes a change in
// screen or audio state.
type ContextUpdate struct {
	Timestamp   time.Time `json:"timestamp"`
	ActiveApp   string    `json:"active_app"`
	WindowTitle string    `json:"window_title"`
	Activity    string    `json:"activity"` // "coding"|"browsing"|"meeting"|"writing"|"idle"|"general"
	OCRText     string    `json:"ocr_text,omitempty"`
	AXSummary   string    `json:"ax_summary,omitempty"`
	Transcript  string    `json:"transcript,omitempty"`
	PixelHash   string    `json:"pixel_hash,omitempty"`
	ChangeKind  string    `json:"change_kind"` // "pattern"|"heartbeat"|"user-invoked"|"error"
	Origin      string    `json:"origin,omitempty"` // e.g. "overlay" - for loopback suppression
}

// UserQuery is sent Overlay -> TUI when the user initiates a message through
// the overlay (wake word, push-to-talk, or panel text input).
type UserQuery struct {
	Text   string `json:"text"`
	Source string `json:"source"` // "wake_word"|"push_to_talk"|"panel_input"
}

// EmberRequest is sent Overlay -> TUI to toggle autonomous ember mode.
type EmberRequest struct {
	Desired string `json:"desired"` // "active"|"inactive"|"paused"|"resumed"
}

// UIEvent is sent Overlay -> TUI for UI interaction telemetry.
type UIEvent struct {
	Kind   string            `json:"kind"`             // "dismiss"|"focus"|"blur"|"expand"
	Target string            `json:"target,omitempty"` // optional component ID
	Meta   map[string]string `json:"meta,omitempty"`
}

// SessionEvent is sent TUI -> Overlay wrapping an internal session.Event so
// the overlay can react to engine events.
type SessionEvent struct {
	Type string          `json:"event_type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// EmberState is sent TUI -> Overlay when ember state changes.
type EmberState struct {
	Active    bool `json:"active"`
	Paused    bool `json:"paused"`
	TickCount int  `json:"tick_count,omitempty"`
}

// AssistantDelta is sent TUI -> Overlay as a streaming text chunk from the
// engine. Finished=true signals end of turn.
type AssistantDelta struct {
	Text     string `json:"text"`
	Finished bool   `json:"finished,omitempty"`
}

// ContextAck is sent TUI -> Overlay on each accepted ContextUpdate so the
// overlay can display a status footer (tokens injected, running total,
// injection mode).
type ContextAck struct {
	Tokens int    `json:"tokens"`
	Reason string `json:"reason,omitempty"`
	Mode   string `json:"mode"` // "system_reminder" | "synthetic_user"
	Total  int    `json:"total_session_tokens"`
}
