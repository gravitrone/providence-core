package direct

import (
	"testing"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDirectEngine(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:         engine.EngineTypeDirect,
		SystemPrompt: "You are a test bot.",
		Model:        "claude-sonnet-4-20250514",
		APIKey:       "test-key-not-real",
	})
	require.NoError(t, err)
	assert.Equal(t, engine.StatusIdle, e.Status())
	assert.NotEmpty(t, e.sessionID)
}

func TestDirectEngine_StatusTransitions(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)

	assert.Equal(t, engine.StatusIdle, e.Status())

	// Can't test full Send without a real API, but we can verify the guard.
	// Manually set to running to test the guard.
	e.mu.Lock()
	e.status = engine.StatusRunning
	e.mu.Unlock()

	err = e.Send("should fail")
	assert.Error(t, err, "should not allow send while running")
}

func TestDirectEngine_EventsChannel(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)

	ch := e.Events()
	assert.NotNil(t, ch)
}

func TestDirectEngine_InterruptIdempotent(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)

	// Should not panic even when called multiple times.
	e.Interrupt()
	e.Interrupt()
}

func TestDirectEngine_Steer(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)

	e.Steer("focus on security")
	e.Steer("also check performance")

	e.steerMu.Lock()
	assert.Len(t, e.steered, 2)
	e.steerMu.Unlock()

	// drainSteeredMessages should move them to history and clear the slice.
	e.drainSteeredMessages()
	e.steerMu.Lock()
	assert.Empty(t, e.steered)
	e.steerMu.Unlock()

	msgs := e.history.Messages()
	assert.Len(t, msgs, 2)
}

func TestDirectEngine_RespondPermission(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)

	// RespondPermission should not error even for unknown question IDs.
	err = e.RespondPermission("nonexistent", "allow")
	assert.NoError(t, err)
}

func TestDirectEngine_RestoreHistory(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)

	// Seed some pre-existing history so we can verify the restore wipes it.
	e.history.AddUser("stale")

	restored := []engine.RestoredMessage{
		{Role: "user", Content: "first user turn"},
		{Role: "assistant", Content: "first assistant reply"},
		{Role: "user", Content: "second user turn"},
		{Role: "assistant", Content: "second assistant reply"},
		// Non user/assistant roles must be silently skipped - they do not
		// map to valid Anthropic API message roles.
		{Role: "system", Content: "should be skipped"},
		{Role: "tool", Content: "should be skipped"},
		{Role: "permission", Content: "should be skipped"},
	}

	require.NoError(t, e.RestoreHistory(restored))

	msgs := e.history.Messages()
	require.Len(t, msgs, 4, "only user/assistant messages should survive restore")

	// Spot check roles and content.
	assert.Equal(t, "first user turn", msgs[0].Content[0].OfText.Text)
	assert.Equal(t, "first assistant reply", msgs[1].Content[0].OfText.Text)
	assert.Equal(t, "second user turn", msgs[2].Content[0].OfText.Text)
	assert.Equal(t, "second assistant reply", msgs[3].Content[0].OfText.Text)

	// Restoring again should replace, not append.
	require.NoError(t, e.RestoreHistory([]engine.RestoredMessage{
		{Role: "user", Content: "fresh"},
	}))
	msgs = e.history.Messages()
	require.Len(t, msgs, 1)
	assert.Equal(t, "fresh", msgs[0].Content[0].OfText.Text)
}
