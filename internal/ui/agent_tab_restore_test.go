package ui

import (
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gravitrone/providence-core/internal/config"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/direct"
)

// captureCfgAsDirect installs a spy factory under engine.EngineTypeDirect that
// stores the EngineConfig it receives into the supplied pointer (under mu) and
// returns a no-op engine satisfying the interface. The original direct factory
// is restored on test cleanup so other parallel tests are not poisoned.
//
// We register a *closeCountingEngine because it already satisfies engine.Engine
// and is local to this package - no need to spin up the real DirectEngine,
// which would require valid credentials and live HTTP clients.
func captureCfgAsDirect(t *testing.T, captured *engine.EngineConfig, mu *sync.Mutex) {
	t.Helper()
	engine.RegisterFactory(engine.EngineTypeDirect, func(cfg engine.EngineConfig) (engine.Engine, error) {
		mu.Lock()
		*captured = cfg
		mu.Unlock()
		return newCloseCountingEngine(), nil
	})
	t.Cleanup(func() {
		// Restore the production direct-engine factory so subsequent tests
		// that exercise real engine creation continue to work.
		engine.RegisterFactory(engine.EngineTypeDirect, func(cfg engine.EngineConfig) (engine.Engine, error) {
			return direct.NewDirectEngine(cfg)
		})
	})
}

// runRestoreCmd runs the tea.Cmd produced by createEngineAndRestore and
// returns the resulting engineRestoredMsg. The cmd encloses the heavy lifting
// inside a closure so we just invoke it and type-assert the result.
func runRestoreCmd(t *testing.T, cmd tea.Cmd) engineRestoredMsg {
	t.Helper()
	require.NotNil(t, cmd, "createEngineAndRestore must return a non-nil tea.Cmd")
	msg := cmd()
	out, ok := msg.(engineRestoredMsg)
	require.True(t, ok, "expected engineRestoredMsg, got %T", msg)
	return out
}

// TestRestoreEnginePreservesOpenRouterProvider guards F-014: createEngineAndRestore
// previously only special-cased codex, so an OpenRouter conversation resumed
// as direct Anthropic Claude. The user's model and route changed silently.
//
// The fix mirrors the OpenRouter branch from createEngineAndSend so resume
// preserves the original provider. The test installs a spy factory, calls
// createEngineAndRestore with an OpenRouter model slug, and asserts the
// EngineConfig that reached the factory has the right provider, type, and
// API key wiring.
func TestRestoreEnginePreservesOpenRouterProvider(t *testing.T) {
	var captured engine.EngineConfig
	var mu sync.Mutex
	captureCfgAsDirect(t, &captured, &mu)

	t.Setenv("OPENROUTER_API_KEY", "sk-or-test-key")

	cmd := createEngineAndRestore(
		nil, // no restored history needed for the provider-selection assertion
		"openai/gpt-5.4",
		engine.EngineTypeClaude, // even though caller asked for claude, the OpenRouter slug must override
		"",
		"",
		config.HooksConfig{},
		false,
		false,
	)
	msg := runRestoreCmd(t, cmd)
	require.NoError(t, msg.err, "factory should succeed for the spy engine")
	require.NotNil(t, msg.engine, "engineRestoredMsg should carry an engine")

	mu.Lock()
	got := captured
	mu.Unlock()

	assert.Equal(t, engine.EngineTypeDirect, got.Type, "OpenRouter must override engine type to direct")
	assert.Equal(t, engine.ProviderOpenRouter, got.Provider, "OpenRouter provider must be set on resume")
	assert.Equal(t, "sk-or-test-key", got.OpenRouterAPIKey, "OpenRouter API key must propagate from env")
	assert.Equal(t, "openai/gpt-5.4", got.Model, "model name must round-trip unchanged")
}

// TestRestoreEnginePreservesOpenRouterAlias verifies the alias path: the user
// stored a session under an OpenRouter alias (e.g. "or-gpt5") rather than the
// canonical "provider/model" slug. SpecFor resolves both forms, so resume must
// still route through openrouter.
func TestRestoreEnginePreservesOpenRouterAlias(t *testing.T) {
	var captured engine.EngineConfig
	var mu sync.Mutex
	captureCfgAsDirect(t, &captured, &mu)

	t.Setenv("OPENROUTER_API_KEY", "sk-or-alias-key")

	cmd := createEngineAndRestore(
		nil,
		"or-gpt5", // alias for "openai/gpt-5.4"
		engine.EngineTypeDirect,
		"",
		"",
		config.HooksConfig{},
		false,
		false,
	)
	msg := runRestoreCmd(t, cmd)
	require.NoError(t, msg.err)

	mu.Lock()
	got := captured
	mu.Unlock()

	assert.Equal(t, engine.ProviderOpenRouter, got.Provider, "alias must resolve to openrouter provider")
	assert.Equal(t, "sk-or-alias-key", got.OpenRouterAPIKey)
}

// TestRestoreEngineCodexBranchStillFires guards against the regression where
// the new OpenRouter branch could shadow or short-circuit the existing codex
// special case. A codex model name must still set Provider="openai" and pull
// access tokens through the auth path (we cannot assert tokens here without a
// real auth flow, but we can verify the provider switch happens).
func TestRestoreEngineCodexBranchStillFires(t *testing.T) {
	var captured engine.EngineConfig
	var mu sync.Mutex
	captureCfgAsDirect(t, &captured, &mu)

	cmd := createEngineAndRestore(
		nil,
		"gpt-5.1-codex-mini", // codex model from the catalog
		engine.EngineTypeClaude,
		"",
		"",
		config.HooksConfig{},
		false,
		false,
	)
	msg := runRestoreCmd(t, cmd)
	require.NoError(t, msg.err)

	mu.Lock()
	got := captured
	mu.Unlock()

	assert.Equal(t, engine.EngineTypeDirect, got.Type, "codex must override engine type to direct")
	assert.Equal(t, engine.ProviderOpenAI, got.Provider, "codex must set openai provider")
}

// TestRestoreEngineDefaultsToAnthropic verifies a plain anthropic model still
// reaches the factory with no provider override, so resume preserves the
// default Anthropic-direct route.
func TestRestoreEngineDefaultsToAnthropic(t *testing.T) {
	var captured engine.EngineConfig
	var mu sync.Mutex
	captureCfgAsDirect(t, &captured, &mu)

	cmd := createEngineAndRestore(
		nil,
		"sonnet",
		engine.EngineTypeDirect,
		"",
		"",
		config.HooksConfig{},
		false,
		false,
	)
	msg := runRestoreCmd(t, cmd)
	require.NoError(t, msg.err)

	mu.Lock()
	got := captured
	mu.Unlock()

	// Provider stays empty for anthropic (the direct engine treats empty as
	// the default Anthropic path). OpenRouter and OpenAI fields stay zero.
	assert.NotEqual(t, engine.ProviderOpenRouter, got.Provider, "anthropic resume must not flip to openrouter")
	assert.NotEqual(t, engine.ProviderOpenAI, got.Provider, "anthropic resume must not flip to openai")
	assert.Empty(t, got.OpenRouterAPIKey, "no openrouter key on anthropic resume")
}
