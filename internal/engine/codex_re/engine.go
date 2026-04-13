package codex_re

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/gravitrone/providence-core/internal/auth"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/direct"
	"github.com/gravitrone/providence-core/internal/engine/session"
)

// EngineTypeCodexRE is the engine type identifier for the Codex RE engine.
const EngineTypeCodexRE engine.EngineType = "codex_re"

func init() {
	engine.RegisterFactory(EngineTypeCodexRE, NewCodexREEngine)
}

// CodexREEngine wraps the Codex session/turn model as a standalone engine.
// For v1, it delegates to the direct engine configured in codex (openai) mode.
// Future versions will implement the full Rust-inspired session architecture.
type CodexREEngine struct {
	inner     engine.Engine
	sessionID string
	model     string

	mu     sync.Mutex
	status engine.SessionStatus
}

// NewCodexREEngine creates a CodexREEngine by constructing a DirectEngine in
// codex mode. This gives us immediate parity with the existing codex provider
// while establishing the engine registration so `/engine codex_re` works.
func NewCodexREEngine(cfg engine.EngineConfig) (engine.Engine, error) {
	// Validate we can get OpenAI tokens before creating the engine.
	tokens, err := auth.EnsureValidOpenAITokens()
	if err != nil {
		return nil, fmt.Errorf("codex RE requires OpenAI OAuth: %w", err)
	}

	model := cfg.Model
	if model == "" {
		model = "gpt-5.4"
	}

	// Delegate to the direct engine in codex mode.
	innerCfg := engine.EngineConfig{
		Type:              engine.EngineTypeDirect,
		SystemPrompt:      cfg.SystemPrompt,
		AllowedTools:      cfg.AllowedTools,
		Model:             model,
		WorkDir:           cfg.WorkDir,
		Provider:          engine.ProviderOpenAI,
		OpenAIAccessToken: tokens.AccessToken,
		OpenAIAccountID:   tokens.AccountID,
	}

	inner, err := direct.NewDirectEngine(innerCfg)
	if err != nil {
		return nil, fmt.Errorf("create codex RE inner engine: %w", err)
	}

	return &CodexREEngine{
		inner:     inner,
		sessionID: uuid.New().String(),
		model:     model,
		status:    engine.StatusIdle,
	}, nil
}

// Send sends a user message to the underlying codex engine.
func (e *CodexREEngine) Send(text string) error {
	e.mu.Lock()
	e.status = engine.StatusRunning
	e.mu.Unlock()
	return e.inner.Send(text)
}

// Events returns the event channel from the underlying engine.
func (e *CodexREEngine) Events() <-chan engine.ParsedEvent {
	return e.inner.Events()
}

// RespondPermission delegates to the underlying engine.
func (e *CodexREEngine) RespondPermission(questionID, optionID string) error {
	return e.inner.RespondPermission(questionID, optionID)
}

// Interrupt sends SIGINT to abort the current turn.
func (e *CodexREEngine) Interrupt() {
	e.inner.Interrupt()
}

// Cancel aborts the current operation.
func (e *CodexREEngine) Cancel() {
	e.inner.Cancel()
	e.mu.Lock()
	e.status = engine.StatusFailed
	e.mu.Unlock()
}

// Close cleanly shuts down the engine.
func (e *CodexREEngine) Close() {
	e.inner.Close()
}

// Status returns the current engine status.
func (e *CodexREEngine) Status() engine.SessionStatus {
	// Prefer the inner engine's status when running, since it tracks
	// completion and failure transitions.
	innerStatus := e.inner.Status()
	if innerStatus != engine.StatusIdle {
		return innerStatus
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

// RestoreHistory delegates history restoration to the inner engine.
func (e *CodexREEngine) RestoreHistory(messages []engine.RestoredMessage) error {
	return e.inner.RestoreHistory(messages)
}

// TriggerCompact delegates compaction to the inner engine.
func (e *CodexREEngine) TriggerCompact(ctx context.Context) error {
	return e.inner.TriggerCompact(ctx)
}

// SessionBus delegates to the inner engine's session bus.
func (e *CodexREEngine) SessionBus() *session.Bus {
	return e.inner.SessionBus()
}
