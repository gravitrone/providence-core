package codex_headless

import (
	"testing"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Factory Registration ---

func TestCodexHeadlessFactoryRegistered(t *testing.T) {
	// The factory should be registered via init(). Attempt creation and verify
	// the error is NOT "unknown engine type" - proving the factory exists.
	_, err := engine.NewEngine(engine.EngineConfig{
		Type: EngineTypeCodexHeadless,
	})
	if err != nil {
		assert.NotContains(t, err.Error(), "unknown engine type")
	}
}

func TestCodexHeadlessCLINotFound(t *testing.T) {
	// Override PATH to guarantee codex is not found.
	e := &CodexHeadlessEngine{}
	_ = e // just verify the struct compiles

	// Test via the factory with an empty PATH - codex should not be found.
	// We can't easily mock LookPath, so verify the error message format
	// when codex IS found vs when it's not.
	cfg := engine.EngineConfig{
		Type:  EngineTypeCodexHeadless,
		Model: "gpt-5.4",
	}
	eng, err := NewCodexHeadlessEngine(cfg)
	if err != nil {
		// codex not installed - expected in CI.
		assert.Contains(t, err.Error(), "codex CLI not installed")
		assert.Contains(t, err.Error(), "codex_re")
		assert.Nil(t, eng)
	} else {
		// codex is installed - engine should be created with defaults.
		require.NotNil(t, eng)
		assert.Equal(t, engine.StatusIdle, eng.Status())
		assert.Equal(t, "gpt-5.4", eng.model)
	}
}

func TestCodexHeadlessDefaultModel(t *testing.T) {
	cfg := engine.EngineConfig{
		Type:  EngineTypeCodexHeadless,
		Model: "", // should default to gpt-5.4
	}
	eng, err := NewCodexHeadlessEngine(cfg)
	if err != nil {
		t.Skip("codex CLI not installed")
	}
	assert.Equal(t, "gpt-5.4", eng.model)
}

func TestCodexHeadlessCustomModel(t *testing.T) {
	cfg := engine.EngineConfig{
		Type:  EngineTypeCodexHeadless,
		Model: "o3",
	}
	eng, err := NewCodexHeadlessEngine(cfg)
	if err != nil {
		t.Skip("codex CLI not installed")
	}
	assert.Equal(t, "o3", eng.model)
}

func TestCodexHeadlessInitialStatus(t *testing.T) {
	cfg := engine.EngineConfig{
		Type: EngineTypeCodexHeadless,
	}
	eng, err := NewCodexHeadlessEngine(cfg)
	if err != nil {
		t.Skip("codex CLI not installed")
	}
	assert.Equal(t, engine.StatusIdle, eng.Status())
}

func TestCodexHeadlessRespondPermissionNoop(t *testing.T) {
	cfg := engine.EngineConfig{
		Type: EngineTypeCodexHeadless,
	}
	eng, err := NewCodexHeadlessEngine(cfg)
	if err != nil {
		t.Skip("codex CLI not installed")
	}
	assert.NoError(t, eng.RespondPermission("q1", "o1"))
}

// TestCodexHeadlessDoesNotImplementHistoryRestorer pins the capability
// split: codex_headless should NOT satisfy HistoryRestorer, so callers
// skip it via the type-assertion pattern rather than calling a stubbed
// no-op that silently drops the requested restore.
func TestCodexHeadlessDoesNotImplementHistoryRestorer(t *testing.T) {
	cfg := engine.EngineConfig{
		Type: EngineTypeCodexHeadless,
	}
	eng, err := NewCodexHeadlessEngine(cfg)
	if err != nil {
		t.Skip("codex CLI not installed")
	}
	var e engine.Engine = eng
	_, ok := e.(engine.HistoryRestorer)
	assert.False(t, ok,
		"codex_headless must not satisfy HistoryRestorer - it has no protocol hook to inject history")
}

// TestCodexHeadlessDoesNotImplementCompactor pins that codex_headless
// opts out of manual compaction. The compaction orchestrator then falls
// back cleanly instead of invoking a stubbed no-op.
func TestCodexHeadlessDoesNotImplementCompactor(t *testing.T) {
	cfg := engine.EngineConfig{
		Type: EngineTypeCodexHeadless,
	}
	eng, err := NewCodexHeadlessEngine(cfg)
	if err != nil {
		t.Skip("codex CLI not installed")
	}
	var e engine.Engine = eng
	_, ok := e.(engine.Compactor)
	assert.False(t, ok,
		"codex_headless must not satisfy Compactor - the codex CLI manages its own context")
}

// TestCodexHeadlessImplementsSessionBusProvider verifies the engine keeps
// a real event bus for background agents. This is the one capability
// codex_headless genuinely supports.
func TestCodexHeadlessImplementsSessionBusProvider(t *testing.T) {
	cfg := engine.EngineConfig{
		Type: EngineTypeCodexHeadless,
	}
	eng, err := NewCodexHeadlessEngine(cfg)
	if err != nil {
		t.Skip("codex CLI not installed")
	}
	var e engine.Engine = eng
	sbp, ok := e.(engine.SessionBusProvider)
	require.True(t, ok, "codex_headless must satisfy SessionBusProvider")
	assert.NotNil(t, sbp.SessionBus())
}
