package engine

// Shared event types emitted by all engine backends.
// Claude-headless-specific types (UserMessage, PermissionResponse, ParseEvent)
// remain in internal/engine/claude/protocol.go.

type ContentPart struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
}

type MessageBody struct {
	Role    string        `json:"role"`
	Content []ContentPart `json:"content"`
}

type Event struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`
}

type SystemInitEvent struct {
	Type      string   `json:"type"`
	Subtype   string   `json:"subtype"`
	SessionID string   `json:"session_id"`
	Tools     []string `json:"tools"`
	Model     string   `json:"model"`
}

type StreamEvent struct {
	Type  string          `json:"type"`
	Event StreamEventData `json:"event"`
}

type StreamEventData struct {
	Type  string       `json:"type"`
	Index int          `json:"index"`
	Delta *StreamDelta `json:"delta,omitempty"`
}

type StreamDelta struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type AssistantEvent struct {
	Type    string       `json:"type"`
	Message AssistantMsg `json:"message"`
}

type AssistantMsg struct {
	Content []ContentPart `json:"content"`
}

type ResultEvent struct {
	Type         string  `json:"type"`
	Subtype      string  `json:"subtype"`
	Result       string  `json:"result"`
	SessionID    string  `json:"session_id"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	IsError      bool    `json:"is_error"`
}

type PermissionRequestEvent struct {
	Type       string             `json:"type"`
	Tool       PermissionTool     `json:"tool"`
	QuestionID string             `json:"question_id"`
	Options    []PermissionOption `json:"options"`
}

type PermissionTool struct {
	Name  string `json:"name"`
	Input any    `json:"input"`
}

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
