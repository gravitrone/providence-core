package engine

import (
	"context"
	"fmt"

	"github.com/gravitrone/providence-core/internal/engine/session"
)

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
	SystemPrompt string         // Legacy: flat string for subagents, tests, headless.
	SystemBlocks []SystemBlock  // Preferred: structured blocks with cache metadata.
	AllowedTools []string
	Model        string
	APIKey       string
	WorkDir      string

	// OpenAI/Codex fields - used when Provider is "openai".
	Provider          string // "anthropic" (default), "openai", or "openrouter"
	OpenAIAccessToken string
	OpenAIAccountID   string

	// OpenRouter fields - used when Provider is "openrouter".
	OpenRouterAPIKey string

	// HooksMap maps event names to hook configs for lifecycle hooks.
	// Populated from config.HooksConfig.ToMap() at startup.
	HooksMap map[string][]HookConfigEntry
}

// HookConfigEntry is a hook definition passed through EngineConfig.
// Mirrors config.HookEntry but lives in the engine package to avoid import cycles.
type HookConfigEntry struct {
	Command string
	URL     string
	Timeout int // milliseconds
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

// Engine is the minimal interface every AI backend must satisfy.
// Capability-specific operations (history restore, manual compaction,
// session bus access) live on separate interfaces below; callers
// feature-detect with a type assertion rather than forcing every engine
// to implement useless stubs.
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
}

// HistoryRestorer is implemented by engines that can rehydrate prior
// conversation turns into their internal history. Callers should
// feature-detect via:
//
//	if hr, ok := eng.(HistoryRestorer); ok { _ = hr.RestoreHistory(msgs) }
//
// Engines that have no hook to inject history (e.g. claude headless,
// codex_headless, opencode) simply do not implement this interface, and
// callers fall back to "resumed session without model-side memory".
type HistoryRestorer interface {
	RestoreHistory(messages []RestoredMessage) error
}

// Compactor is implemented by engines that support manual context
// compaction. Engines without a compaction hook omit this interface; the
// compaction orchestrator then skips them or falls back to a different
// strategy (e.g. context collapse) rather than hanging on a stub error.
type Compactor interface {
	TriggerCompact(ctx context.Context) error
}

// SessionBusProvider is implemented by engines that publish lifecycle
// events onto a fan-out bus for background agents, overlays, and
// plugin subscribers. Engines without a real bus omit this interface so
// subscribers can detect "no bus" upfront instead of draining a
// throwaway channel that never fires.
type SessionBusProvider interface {
	SessionBus() *session.Bus
}

// TodoItem mirrors the direct/tools TodoItem for cross-package access.
type TodoItem struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
	ParentID string `json:"parentId"`
}

// TodoProvider is optionally implemented by engines that support TodoWrite.
type TodoProvider interface {
	GetCurrentTodos() []TodoItem
}

// CollapseProvider is optionally implemented by engines that support
// lightweight context collapse (summarizing old tool-result groups in place)
// as a cheaper alternative to full API-based compaction.
type CollapseProvider interface {
	// TriggerCollapse runs context collapse on the conversation history,
	// returning the number of tool-result blocks collapsed.
	TriggerCollapse() (int, error)
}

// ParsedEvent is a decoded event from an AI engine.
type ParsedEvent struct {
	Type string
	Data any
	Raw  string
	Err  error
}

// UsageUpdateEvent carries token usage totals from a provider response.
type UsageUpdateEvent struct {
	Type              string `json:"type"`
	InputTokens       int    `json:"input_tokens"`
	OutputTokens      int    `json:"output_tokens"`
	TotalTokens       int    `json:"total_tokens"`
	CacheReadTokens   int    `json:"cache_read_tokens,omitempty"`
	CacheCreateTokens int    `json:"cache_create_tokens,omitempty"`
}

// CompactionEvent carries lifecycle updates from the async compaction pipeline.
type CompactionEvent struct {
	Type         string
	Phase        string
	TokensBefore int
	TokensAfter  int
	Err          error
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
