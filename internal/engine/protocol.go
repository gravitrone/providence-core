package engine

// Shared event types emitted by all engine backends.
// Claude-headless-specific types (UserMessage, PermissionResponse, ParseEvent)
// remain in internal/engine/claude/protocol.go.

// --- Message Types ---

// ContentPart is a single element in a message content array (text, tool_use, tool_result).
type ContentPart struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
}

// MessageBody is a structured message with a role and content parts.
type MessageBody struct {
	Role    string        `json:"role"`
	Content []ContentPart `json:"content"`
}

// --- Event Types ---

// Event is the base envelope for all NDJSON events (type + subtype).
type Event struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`
}

// SystemInitEvent is emitted on session startup to announce capabilities.
type SystemInitEvent struct {
	Type      string   `json:"type"`
	Subtype   string   `json:"subtype"`
	SessionID string   `json:"session_id"`
	Tools     []string `json:"tools"`
	Model     string   `json:"model"`
}

// StreamEvent wraps a streaming API delta event.
type StreamEvent struct {
	Type  string          `json:"type"`
	Event StreamEventData `json:"event"`
}

// StreamEventData carries the delta payload inside a StreamEvent.
type StreamEventData struct {
	Type  string       `json:"type"`
	Index int          `json:"index"`
	Delta *StreamDelta `json:"delta,omitempty"`
}

// StreamDelta is a text or tool-input fragment from a streaming response.
type StreamDelta struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// AssistantEvent wraps a complete assistant message turn.
type AssistantEvent struct {
	Type    string       `json:"type"`
	Message AssistantMsg `json:"message"`
}

// AssistantMsg is the content payload of an assistant turn.
type AssistantMsg struct {
	Content []ContentPart `json:"content"`
}

// ResultEvent is emitted at turn completion with outcome and cost metrics.
type ResultEvent struct {
	Type         string  `json:"type"`
	Subtype      string  `json:"subtype"`
	Result       string  `json:"result"`
	SessionID    string  `json:"session_id"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	IsError      bool    `json:"is_error"`
}

// PermissionRequestEvent is emitted when the engine needs user approval for a tool.
type PermissionRequestEvent struct {
	Type       string             `json:"type"`
	Tool       PermissionTool     `json:"tool"`
	QuestionID string             `json:"question_id"`
	Options    []PermissionOption `json:"options"`
}

// PermissionTool describes the tool awaiting permission.
type PermissionTool struct {
	Name  string `json:"name"`
	Input any    `json:"input"`
}

// PermissionOption is a selectable response to a permission request.
type PermissionOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// ToolResultEvent carries the output of a completed tool execution to the UI.
type ToolResultEvent struct {
	Type       string `json:"type"`
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Output     string `json:"output"`
	IsError    bool   `json:"is_error"`
}

// TombstoneEvent signals the UI to remove a partial assistant message from the
// transcript, e.g. after a model overload triggers a fallback retry.
type TombstoneEvent struct {
	Type         string `json:"type"`
	MessageIndex int    `json:"message_index"`
}

// SystemMessageEvent carries an informational system message to the UI.
type SystemMessageEvent struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// ToolInputDelta carries a partial JSON fragment for a streaming tool input.
type ToolInputDelta struct {
	Type        string `json:"type"`
	PartialJSON string `json:"partial_json"`
}

// ThinkingStartEvent signals the beginning of a thinking block.
type ThinkingStartEvent struct {
	Type string `json:"type"`
}

// ThinkingDelta carries a partial text fragment from a thinking block.
type ThinkingDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ThinkingStopEvent signals the end of a thinking block.
type ThinkingStopEvent struct {
	Type string `json:"type"`
}

// RateLimitEvent is emitted when a 429 is encountered and the engine is retrying.
// The UI can use DelaySec and Attempt to show a live countdown.
type RateLimitEvent struct {
	Type     string `json:"type"`
	DelaySec int    `json:"delay_sec"` // total seconds until next retry
	Attempt  int    `json:"attempt"`   // 1-indexed attempt number
	MaxRetry int    `json:"max_retry"` // max retries (e.g. 3)
}
