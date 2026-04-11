package engine

import "fmt"

// EngineType identifies the backend.
type EngineType string

const (
	EngineTypeClaude EngineType = "claude"
	EngineTypeDirect EngineType = "direct"
)

// Provider identifiers for use with EngineConfig.Provider.
const (
	ProviderAnthropic  = "anthropic"
	ProviderOpenAI     = "openai"
	ProviderOpenRouter = "openrouter"
)

// EngineConfig holds creation parameters for any engine.
type EngineConfig struct {
	Type         EngineType
	SystemPrompt string
	AllowedTools []string
	Model        string
	APIKey       string
	WorkDir      string

	// OpenAI/Codex fields - used when Provider is "openai".
	Provider           string // "anthropic" (default), "openai", or "openrouter"
	OpenAIAccessToken  string
	OpenAIAccountID    string

	// OpenRouter fields - used when Provider is "openrouter".
	OpenRouterAPIKey string
}

// RestoredMessage is the persisted message shape used to rehydrate engine
// history from a stored session. Tool metadata is optional and additive so
// older persisted rows remain valid.
type RestoredMessage struct {
	Role    string
	Content string
	// Tool metadata: ToolCallID, ToolName, and ToolInput are optional fields
	// populated when restoring tool rows (Role == "tool") to allow engines to
	// synthesize prior tool outcomes back into model-visible context.
	ToolCallID string
	ToolName   string
	ToolInput  string
}

// Engine is the interface all AI backends must implement.
type Engine interface {
	// Send sends a user message to the AI.
	Send(text string) error
	// Events returns a channel of parsed events from the AI.
	Events() <-chan ParsedEvent
	// RespondPermission responds to a permission request.
	RespondPermission(questionID, optionID string) error
	// Interrupt sends SIGINT to abort the current turn without killing the session.
	// The engine should emit a result event and remain usable for the next message.
	Interrupt()
	// Cancel aborts the current operation and kills the session.
	Cancel()
	// Close cleanly shuts down the engine.
	Close()
	// Status returns the current engine status.
	Status() SessionStatus
	// RestoreHistory replaces the engine's conversation history with the given
	// messages. Used when resuming a past session so the model has memory of
	// prior turns. Engines may synthesize text for tool history when they
	// cannot replay native tool-call blocks safely.
	// Engines that cannot inject history (e.g. claude headless) should
	// implement this as a no-op.
	RestoreHistory(messages []RestoredMessage) error
}

// ParsedEvent is a decoded event from an AI engine.
type ParsedEvent struct {
	Type string
	Data any
	Raw  string
	Err  error
}

// SessionStatus represents the lifecycle state of an engine session.
type SessionStatus int

const (
	StatusIdle SessionStatus = iota
	StatusConnecting
	StatusRunning
	StatusCompleted
	StatusFailed
)

// EngineFactory is a constructor function that creates an Engine from config.
type EngineFactory func(cfg EngineConfig) (Engine, error)

var factories = map[EngineType]EngineFactory{}

// RegisterFactory registers a factory for the given engine type.
// Call this from an init() in each engine subpackage.
func RegisterFactory(t EngineType, f EngineFactory) {
	factories[t] = f
}

// NewEngine creates an Engine based on the given config type.
func NewEngine(cfg EngineConfig) (Engine, error) {
	f, ok := factories[cfg.Type]
	if !ok {
		return nil, fmt.Errorf("unknown engine type: %s", cfg.Type)
	}
	return f(cfg)
}
