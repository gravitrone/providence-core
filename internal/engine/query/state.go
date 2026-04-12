package query

// State holds mutable state across loop iterations.
type State struct {
	Messages                     []Message
	TurnCount                    int
	MaxOutputTokensRecoveryCount int
	HasAttemptedReactiveCompact  bool
	PendingToolUseSummary        *string
	AutoCompactTracking          *AutoCompactTracking
}

// AutoCompactTracking tracks consecutive compaction failures to avoid
// infinite retry loops.
type AutoCompactTracking struct {
	ConsecutiveFailures int
}

// Message is a provider-agnostic message representation. Every engine
// converts to/from this before hitting the unified query loop.
type Message struct {
	Role    string // "user", "assistant", "system", "tool_result"
	Content string

	// Tool call fields - populated when Role is "assistant" with a tool
	// invocation, or "tool_result" with the outcome.
	ToolCalls []ToolCall

	// Tool result fields - populated for role "tool_result".
	ToolCallID string
	ToolName   string
	ToolInput  string

	// Metadata
	IsMeta bool
	UUID   string
}

// ToolCall represents a single tool invocation requested by the model.
type ToolCall struct {
	ID    string
	Name  string
	Input string
}
