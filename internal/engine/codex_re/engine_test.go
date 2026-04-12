package codex_re

import (
	"context"
	"testing"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodexREEngineFactoryRegistered(t *testing.T) {
	// The factory should be registered via init(). Attempt creation and verify
	// the error is NOT "unknown engine type" - proving the factory is registered.
	// On machines with valid OpenAI tokens this may succeed; on CI it will fail
	// with an auth error. Either way, the factory itself is registered.
	_, err := engine.NewEngine(engine.EngineConfig{
		Type: EngineTypeCodexRE,
	})
	if err != nil {
		assert.NotContains(t, err.Error(), "unknown engine type")
	}
}

func TestCodexREEngineCreation(t *testing.T) {
	// If valid tokens exist, full creation should succeed.
	eng, err := engine.NewEngine(engine.EngineConfig{
		Type:  EngineTypeCodexRE,
		Model: "gpt-5.4",
	})
	if err != nil {
		// Auth not available - verify the error references OAuth.
		assert.Contains(t, err.Error(), "OpenAI OAuth")
		return
	}
	require.NotNil(t, eng)
	assert.Equal(t, engine.StatusIdle, eng.Status())
}

func TestCodexREEngineStatus(t *testing.T) {
	// Construct directly to test status without needing auth.
	e := &CodexREEngine{
		status: engine.StatusIdle,
		inner:  &stubEngine{status: engine.StatusIdle},
	}
	assert.Equal(t, engine.StatusIdle, e.Status())
}

func TestCodexREEngineStatusDelegates(t *testing.T) {
	// When inner engine reports non-idle, that should be returned.
	e := &CodexREEngine{
		status: engine.StatusIdle,
		inner:  &stubEngine{status: engine.StatusRunning},
	}
	assert.Equal(t, engine.StatusRunning, e.Status())
}

// stubEngine is a minimal Engine for testing without auth.
type stubEngine struct {
	status engine.SessionStatus
}

func (s *stubEngine) Send(string) error                            { return nil }
func (s *stubEngine) Events() <-chan engine.ParsedEvent             { return make(chan engine.ParsedEvent) }
func (s *stubEngine) RespondPermission(string, string) error        { return nil }
func (s *stubEngine) Interrupt()                                    {}
func (s *stubEngine) Cancel()                                       {}
func (s *stubEngine) Close()                                        {}
func (s *stubEngine) Status() engine.SessionStatus                  { return s.status }
func (s *stubEngine) RestoreHistory([]engine.RestoredMessage) error { return nil }
func (s *stubEngine) TriggerCompact(_ context.Context) error        { return nil }
