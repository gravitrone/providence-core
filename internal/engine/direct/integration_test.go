package direct

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/compact"
	"github.com/gravitrone/providence-core/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- TestSessionPersistAndResume ---

func TestSessionPersistAndResume(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	require.NoError(t, err)
	defer st.Close()

	sessionID := "integration-test-session"
	require.NoError(t, st.CreateSession(sessionID, "/tmp/project", "direct", "sonnet"))

	// Add messages.
	_, err = st.AddMessage(sessionID, "user", "hello world", "", "", "", "", "", 0, true)
	require.NoError(t, err)
	_, err = st.AddMessage(sessionID, "assistant", "hi there", "", "", "", "", "", 0, true)
	require.NoError(t, err)
	_, err = st.AddMessage(sessionID, "tool", "package main", "ReadFile", "main.go", "success", "", "package main", 0, true)
	require.NoError(t, err)

	// Round-trip: read messages back.
	msgs, err := st.GetMessages(sessionID)
	require.NoError(t, err)
	require.Len(t, msgs, 3)

	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "hello world", msgs[0].Content)
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "hi there", msgs[1].Content)
	assert.Equal(t, "tool", msgs[2].Role)
	assert.Equal(t, "ReadFile", msgs[2].ToolName)

	// Build RestoredMessage list from persisted rows.
	restored := make([]engine.RestoredMessage, len(msgs))
	for i, m := range msgs {
		restored[i] = engine.RestoredMessage{
			Role:      m.Role,
			Content:   m.Content,
			ToolName:  m.ToolName,
			ToolInput: m.ToolArgs,
		}
	}

	// Verify format matches what RestoreHistory expects.
	require.Len(t, restored, 3)
	assert.Equal(t, "user", restored[0].Role)
	assert.Equal(t, "hello world", restored[0].Content)
	assert.Equal(t, "tool", restored[2].Role)
	assert.Equal(t, "ReadFile", restored[2].ToolName)
	assert.Equal(t, "main.go", restored[2].ToolInput)

	// Verify engine can consume restored messages.
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)
	require.NoError(t, e.RestoreHistory(restored))

	history := e.history.Messages()
	require.Len(t, history, 3)
	assert.Equal(t, "hello world", history[0].Content[0].OfText.Text)
}

// --- TestCompactionThresholdMath ---

func TestCompactionThresholdMath(t *testing.T) {
	tests := []struct {
		name              string
		contextWindow     int
		maxOutputTokens   int
		wantEffective     int
		wantAutoThreshold int
		wantBlocking      int
	}{
		{
			name:              "standard anthropic model",
			contextWindow:     200000,
			maxOutputTokens:   8192,
			wantEffective:     200000 - 8192,
			wantAutoThreshold: 200000 - 8192 - 13000,
			wantBlocking:      200000 - 8192 - 3000,
		},
		{
			name:              "large output model",
			contextWindow:     200000,
			maxOutputTokens:   16384,
			wantEffective:     200000 - 16384,
			wantAutoThreshold: 200000 - 16384 - 13000,
			wantBlocking:      200000 - 16384 - 3000,
		},
		{
			name:              "huge output capped at 20000",
			contextWindow:     200000,
			maxOutputTokens:   50000,
			wantEffective:     200000 - 20000,
			wantAutoThreshold: 200000 - 20000 - 13000,
			wantBlocking:      200000 - 20000 - 3000,
		},
		{
			name:              "small context window",
			contextWindow:     64000,
			maxOutputTokens:   8192,
			wantEffective:     64000 - 8192,
			wantAutoThreshold: 64000 - 8192 - 13000,
			wantBlocking:      64000 - 8192 - 3000,
		},
		{
			name:              "gemini 1M window",
			contextWindow:     1000000,
			maxOutputTokens:   8192,
			wantEffective:     1000000 - 8192,
			wantAutoThreshold: 1000000 - 8192 - 13000,
			wantBlocking:      1000000 - 8192 - 3000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			effective := compact.GetEffectiveContextWindow(tt.contextWindow, tt.maxOutputTokens)
			assert.Equal(t, tt.wantEffective, effective, "effective context window")

			autoThreshold := compact.GetAutoCompactThreshold(tt.contextWindow, tt.maxOutputTokens)
			assert.Equal(t, tt.wantAutoThreshold, autoThreshold, "auto compact threshold")

			blocking := compact.GetBlockingLimit(tt.contextWindow, tt.maxOutputTokens)
			assert.Equal(t, tt.wantBlocking, blocking, "blocking limit")

			// Invariants.
			assert.Greater(t, effective, autoThreshold, "effective > auto threshold")
			assert.Greater(t, blocking, autoThreshold, "blocking > auto threshold")
			assert.Greater(t, effective, 0, "effective > 0")
		})
	}
}

// --- TestToolRegistryComplete ---

func TestToolRegistryComplete(t *testing.T) {
	e, err := NewDirectEngine(engine.EngineConfig{
		Type:   engine.EngineTypeDirect,
		Model:  "claude-sonnet-4-20250514",
		APIKey: "test-key-not-real",
	})
	require.NoError(t, err)

	allTools := e.registry.All()
	toolNames := make(map[string]bool, len(allTools))
	for _, tool := range allTools {
		toolNames[tool.Name()] = true
	}

	// Core tools that must always be registered.
	coreExpected := []string{
		"Read", "Write", "Edit", "Bash", "Glob", "Grep",
		"WebFetch", "WebSearch", "Agent", "TodoWrite",
		"AskUserQuestion", "EnterPlanMode", "ExitPlanMode",
	}

	for _, name := range coreExpected {
		assert.True(t, toolNames[name], "expected tool %q to be registered", name)
	}

	// Darwin-only tools.
	if runtime.GOOS == "darwin" {
		darwinExpected := []string{
			"Screenshot", "DesktopClick", "DesktopType", "DesktopApps", "Clipboard",
		}
		for _, name := range darwinExpected {
			assert.True(t, toolNames[name], "expected darwin tool %q to be registered", name)
		}
	}

	// Verify each tool has a non-empty description and schema.
	for _, tool := range allTools {
		assert.NotEmpty(t, tool.Name(), "tool name should not be empty")
		assert.NotEmpty(t, tool.Description(), "tool %q should have a description", tool.Name())
		schema := tool.InputSchema()
		assert.NotNil(t, schema, "tool %q should have an input schema", tool.Name())
	}
}

// --- TestConversationHistoryOperations ---

func TestConversationHistoryOperations(t *testing.T) {
	h := NewConversationHistory()

	// Empty history.
	assert.Empty(t, h.Messages())
	assert.Equal(t, 0, h.EstimateTokens())
	assert.Equal(t, 0, h.CurrentTokens())

	// Add messages.
	h.AddUser("hello world")
	h.AddAssistantText("hi there, how can I help?")

	msgs := h.Messages()
	require.Len(t, msgs, 2)

	// EstimateTokens should return > 0 for non-empty history.
	estimate := h.EstimateTokens()
	assert.Greater(t, estimate, 0, "estimate should be > 0 for non-empty history")

	// CurrentTokens should fall back to estimate when no reported tokens.
	assert.Equal(t, estimate, h.CurrentTokens(), "CurrentTokens should fall back to estimate")

	// SetReportedTokens should override estimate.
	h.SetReportedTokens(500, 200)
	assert.Equal(t, 700, h.CurrentTokens(), "CurrentTokens should return reported total")

	// Estimate should remain unchanged (it's based on content, not reported).
	assert.Equal(t, estimate, h.EstimateTokens(), "EstimateTokens should not change after SetReportedTokens")

	// CompressLongToolResults with short content should compress nothing.
	compressed := h.CompressLongToolResults(2000)
	assert.Equal(t, 0, compressed, "no tool results to compress")
}

func TestCompressLongToolResultsIntegration(t *testing.T) {
	h := NewConversationHistory()

	// Need > 4 messages for compression to activate.
	h.AddUser("msg1")
	h.AddAssistantText("reply1")
	h.AddUser("msg2")
	h.AddAssistantText("reply2")
	h.AddUser("msg3")
	h.AddAssistantText("reply3")

	// With no tool results, compression count should be 0.
	compressed := h.CompressLongToolResults(100)
	assert.Equal(t, 0, compressed)

	// Verify reported tokens reset on compression with actual tool results.
	h.SetReportedTokens(1000, 500)
	assert.Equal(t, 1500, h.CurrentTokens())
}

// --- TestModelCatalogComplete ---

func TestModelCatalogComplete(t *testing.T) {
	require.GreaterOrEqual(t, len(engine.ModelCatalog), 15, "catalog should have at least 15 models")

	for _, spec := range engine.ModelCatalog {
		t.Run(spec.Name, func(t *testing.T) {
			assert.NotEmpty(t, spec.Name, "model name should not be empty")
			assert.NotEmpty(t, spec.Display, "model display should not be empty")
			assert.NotEmpty(t, spec.Provider, "model provider should not be empty")
			assert.Greater(t, spec.ContextWindow, 0, "context window should be > 0")
			assert.Greater(t, spec.MaxOutputTokens, 0, "max output tokens should be > 0")

			// TierFor should return a valid tier.
			tier := engine.TierFor(spec.Name)
			assert.Contains(t, []engine.ModelTier{engine.TierFast, engine.TierMedium, engine.TierCapable}, tier)

			// ContextWindowFor should match the spec.
			assert.Equal(t, spec.ContextWindow, engine.ContextWindowFor(spec.Name))
		})
	}

	// FastForProvider should return a model for each major provider.
	for _, provider := range []string{"anthropic", "openai", "openrouter"} {
		fast := engine.FastForProvider(provider)
		assert.NotEmpty(t, fast, "FastForProvider(%q) should return a model", provider)
	}

	// ResolveAlias tests.
	assert.Equal(t, "claude-haiku-4-5-20251001", engine.ResolveAlias("haiku"))
	assert.Equal(t, "gpt-5.4", engine.ResolveAlias("codex"))
	assert.Equal(t, "claude-sonnet-4-6", engine.ResolveAlias("sonnet"))
	assert.Equal(t, "claude-opus-4-6", engine.ResolveAlias("opus"))

	// Unknown alias should pass through unchanged.
	assert.Equal(t, "unknown-model", engine.ResolveAlias("unknown-model"))
}
