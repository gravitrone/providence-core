package query

import "context"

// Deps holds all injectable dependencies for the query loop.
type Deps struct {
	Provider     Provider
	Tools        ToolExecutor
	Compact      Compactor
	Hooks        HookRunner
	Permissions  PermissionChecker
	SystemPrompt string
	MaxTurns     int // 0 = unlimited
	TokenBudget  int // 0 = unlimited
}

// Provider abstracts the AI API backend. Each engine (anthropic, codex,
// openrouter) implements this once, and the unified loop handles the rest.
type Provider interface {
	// Stream sends messages to the model and returns streaming events.
	Stream(ctx context.Context, messages []Message, tools []ToolDef, systemPrompt string) (<-chan StreamEvent, error)
	// OneShot makes a single non-streaming completion call.
	OneShot(ctx context.Context, systemPrompt, userPrompt string) (string, error)
	// Model returns the current model name.
	Model() string
	// ContextWindow returns the max context size in tokens.
	ContextWindow() int
	// MaxOutputTokens returns the max output token limit.
	MaxOutputTokens() int
}

// StreamEvent represents a streaming API event from any provider.
type StreamEvent struct {
	Type         string // "text_delta", "tool_use_start", "tool_use_delta", "tool_use_stop", "message_complete", "error"
	Text         string
	ToolUseID    string
	ToolName     string
	ToolInput    string
	StopReason   string
	Error        error
	InputTokens  int
	OutputTokens int
}

// ToolExecutor handles tool execution.
type ToolExecutor interface {
	// Execute runs a tool by name with the given JSON input, returning the result.
	Execute(ctx context.Context, name string, input string) (string, error)
	// IsConcurrencySafe reports whether the named tool can run in parallel
	// with other safe tools (e.g. read-only tools).
	IsConcurrencySafe(name string) bool
	// ListTools returns all available tool definitions for the API.
	ListTools() []ToolDef
}

// ToolDef is a tool definition sent to the API.
type ToolDef struct {
	Name        string
	Description string
	InputSchema interface{}
}

// Compactor handles context compaction - both auto-triggered and reactive.
type Compactor interface {
	// TriggerIfNeeded checks token usage and starts background compaction
	// if the context is getting large. Returns true if compaction was triggered.
	TriggerIfNeeded(ctx context.Context) bool
	// TriggerReactive forces an immediate compaction, blocking until done.
	TriggerReactive(ctx context.Context) error
	// WaitForPending blocks until any in-flight compaction completes.
	WaitForPending(ctx context.Context) error
}

// HookRunner fires hooks at lifecycle points (pre-tool, post-tool, etc).
type HookRunner interface {
	Run(ctx context.Context, event string, data interface{}) error
}

// PermissionChecker validates whether a tool invocation is allowed.
type PermissionChecker interface {
	Check(toolName string, input string) (bool, error)
}
