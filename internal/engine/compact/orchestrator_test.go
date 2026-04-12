package compact

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProvider struct {
	currentTokens   int
	contextWindow   int
	maxOutputTokens int

	compressFn  func(context.Context, int) (int, error)
	serializeFn func(int) (string, int, error)
	oneShotFn   func(context.Context, string, string) (string, error)
	replaceFn   func(string, int) error

	compressCalls  int
	serializeCalls int
	oneShotCalls   int
	replaceCalls   int
}

func (m *mockProvider) Compress(ctx context.Context, keepRecentTokens int) (int, error) {
	m.compressCalls++
	if m.compressFn != nil {
		return m.compressFn(ctx, keepRecentTokens)
	}
	return 0, nil
}

func (m *mockProvider) Serialize(keepRecentTokens int) (string, int, error) {
	m.serializeCalls++
	if m.serializeFn != nil {
		return m.serializeFn(keepRecentTokens)
	}
	return "", 0, nil
}

func (m *mockProvider) Replace(summary string, cutIndex int) error {
	m.replaceCalls++
	if m.replaceFn != nil {
		return m.replaceFn(summary, cutIndex)
	}
	return nil
}

func (m *mockProvider) OneShot(ctx context.Context, systemPrompt, input string) (string, error) {
	m.oneShotCalls++
	if m.oneShotFn != nil {
		return m.oneShotFn(ctx, systemPrompt, input)
	}
	return "", nil
}

func (m *mockProvider) CurrentTokens() int {
	return m.currentTokens
}

func (m *mockProvider) ContextWindow() int {
	return m.contextWindow
}

func (m *mockProvider) MaxOutputTokens() int {
	if m.maxOutputTokens <= 0 {
		return 8192
	}
	return m.maxOutputTokens
}

func TestTriggerIfNeededBelowThreshold_NoOp(t *testing.T) {
	t.Parallel()

	// With 200K window and 8192 max output, threshold = 200000 - 8192 - 13000 = 178808
	provider := &mockProvider{
		currentTokens:   178807,
		contextWindow:   200000,
		maxOutputTokens: 8192,
	}

	orchestrator := New(provider, nil)

	assert.False(t, orchestrator.TriggerIfNeeded(context.Background()))
	assert.False(t, orchestrator.IsRunning())
	assert.Zero(t, provider.compressCalls)
}

func TestTriggerIfNeededAboveThreshold_StartsAsync(t *testing.T) {
	t.Parallel()

	// threshold = 200000 - 8192 - 13000 = 178808
	release := make(chan struct{})
	provider := &mockProvider{
		currentTokens:   178808,
		contextWindow:   200000,
		maxOutputTokens: 8192,
		serializeFn: func(keepRecentTokens int) (string, int, error) {
			<-release
			return "history", 2, nil
		},
		oneShotFn: func(ctx context.Context, systemPrompt, input string) (string, error) {
			return "summary", nil
		},
	}

	orchestrator := New(provider, nil)

	assert.True(t, orchestrator.TriggerIfNeeded(context.Background()))
	assert.True(t, orchestrator.IsRunning())

	close(release)
	require.NoError(t, orchestrator.WaitForPending(context.Background()))
	assert.False(t, orchestrator.IsRunning())
}

func TestTriggerNowIgnoresThreshold(t *testing.T) {
	t.Parallel()

	provider := &mockProvider{
		currentTokens: 10,
		contextWindow: 1000,
		serializeFn: func(keepRecentTokens int) (string, int, error) {
			return "history", 3, nil
		},
		oneShotFn: func(ctx context.Context, systemPrompt, input string) (string, error) {
			return "summary", nil
		},
	}

	orchestrator := New(provider, nil)

	assert.True(t, orchestrator.TriggerNow(context.Background()))
	require.NoError(t, orchestrator.WaitForPending(context.Background()))
	assert.Equal(t, 1, provider.replaceCalls)
}

func TestWaitForPendingRunsGenericPipeline(t *testing.T) {
	t.Parallel()

	var phases []Phase
	provider := &mockProvider{
		currentTokens: 900,
		contextWindow: 1000,
		serializeFn: func(keepRecentTokens int) (string, int, error) {
			assert.Equal(t, 300, keepRecentTokens)
			return "serialized history", 4, nil
		},
		oneShotFn: func(ctx context.Context, systemPrompt, input string) (string, error) {
			assert.Equal(t, SystemPrompt, systemPrompt)
			assert.Equal(t, "serialized history", input)
			return "summary text", nil
		},
		replaceFn: func(summary string, cutIndex int) error {
			assert.Equal(t, "summary text", summary)
			assert.Equal(t, 4, cutIndex)
			return nil
		},
	}

	orchestrator := New(provider, func(phase Phase, err error) {
		phases = append(phases, phase)
	})

	assert.True(t, orchestrator.TriggerNow(context.Background()))
	require.NoError(t, orchestrator.WaitForPending(context.Background()))
	assert.Equal(t, []Phase{PhaseRunning, PhaseReady, PhaseIdle}, phases)
	assert.Equal(t, 1, provider.serializeCalls)
	assert.Equal(t, 1, provider.oneShotCalls)
	assert.Equal(t, 1, provider.replaceCalls)
}

func TestWaitForPendingUsesProviderCompressFastPath(t *testing.T) {
	t.Parallel()

	provider := &mockProvider{
		currentTokens: 900,
		contextWindow: 1000,
		compressFn: func(ctx context.Context, keepRecentTokens int) (int, error) {
			assert.Equal(t, 300, keepRecentTokens)
			return 2, nil
		},
	}

	orchestrator := New(provider, nil)

	assert.True(t, orchestrator.TriggerNow(context.Background()))
	require.NoError(t, orchestrator.WaitForPending(context.Background()))
	assert.Equal(t, 1, provider.compressCalls)
	assert.Zero(t, provider.serializeCalls)
	assert.Zero(t, provider.oneShotCalls)
	assert.Zero(t, provider.replaceCalls)
}

func TestWaitForPendingSurfacesOneShotFailure(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("boom")
	var phases []Phase
	provider := &mockProvider{
		currentTokens: 900,
		contextWindow: 1000,
		serializeFn: func(keepRecentTokens int) (string, int, error) {
			return "history", 2, nil
		},
		oneShotFn: func(ctx context.Context, systemPrompt, input string) (string, error) {
			return "", expectedErr
		},
	}

	orchestrator := New(provider, func(phase Phase, err error) {
		phases = append(phases, phase)
	})

	assert.True(t, orchestrator.TriggerNow(context.Background()))
	err := orchestrator.WaitForPending(context.Background())
	require.ErrorIs(t, err, expectedErr)
	assert.Equal(t, []Phase{PhaseRunning, PhaseFailed}, phases)
	assert.Equal(t, PhaseIdle, orchestrator.phase)
}

func TestTriggerNowWhileRunningDoesNotStartTwice(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	provider := &mockProvider{
		currentTokens: 900,
		contextWindow: 1000,
		serializeFn: func(keepRecentTokens int) (string, int, error) {
			<-release
			return "history", 2, nil
		},
		oneShotFn: func(ctx context.Context, systemPrompt, input string) (string, error) {
			return "summary", nil
		},
	}

	orchestrator := New(provider, nil)

	assert.True(t, orchestrator.TriggerNow(context.Background()))
	assert.False(t, orchestrator.TriggerNow(context.Background()))

	close(release)
	require.NoError(t, orchestrator.WaitForPending(context.Background()))
	assert.Equal(t, 1, provider.serializeCalls)
}

// --- Threshold Formula Tests ---

func TestGetEffectiveContextWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		contextWindow int
		maxOutput     int
		expected      int
	}{
		{"200K window, 8192 output", 200000, 8192, 200000 - 8192},
		{"200K window, 64000 output (capped at 20K)", 200000, 64000, 200000 - 20000},
		{"1M window, 8192 output", 1000000, 8192, 1000000 - 8192},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, GetEffectiveContextWindow(tt.contextWindow, tt.maxOutput))
		})
	}
}

func TestGetAutoCompactThreshold(t *testing.T) {
	t.Parallel()

	// 200K anthropic: effective = 200000 - 8192 = 191808, threshold = 191808 - 13000 = 178808
	threshold := GetAutoCompactThreshold(200000, 8192)
	assert.Equal(t, 178808, threshold)

	// ~89.4% of 200K - correct, not the old hardcoded 80%
	pct := float64(threshold) / float64(200000) * 100
	assert.InDelta(t, 89.4, pct, 0.1)
}

func TestGetBlockingLimit(t *testing.T) {
	t.Parallel()

	// 200K anthropic: effective = 191808, blocking = 191808 - 3000 = 188808
	limit := GetBlockingLimit(200000, 8192)
	assert.Equal(t, 188808, limit)
}

// --- Circuit Breaker Tests ---

func TestCircuitBreakerTripsAfterMaxFailures(t *testing.T) {
	t.Parallel()

	provider := &mockProvider{
		currentTokens:   190000,
		contextWindow:   200000,
		maxOutputTokens: 8192,
		serializeFn: func(keepRecentTokens int) (string, int, error) {
			return "history", 2, nil
		},
		oneShotFn: func(ctx context.Context, systemPrompt, input string) (string, error) {
			return "", errors.New("api error")
		},
	}

	orchestrator := New(provider, nil)

	// Fail 3 times
	for i := 0; i < MaxConsecutiveFailures; i++ {
		assert.True(t, orchestrator.TriggerNow(context.Background()))
		_ = orchestrator.WaitForPending(context.Background())
	}

	// Circuit breaker should be tripped - TriggerIfNeeded returns false
	assert.False(t, orchestrator.TriggerIfNeeded(context.Background()))
}

func TestCircuitBreakerResetsOnSuccess(t *testing.T) {
	t.Parallel()

	failCount := 0
	provider := &mockProvider{
		currentTokens:   190000,
		contextWindow:   200000,
		maxOutputTokens: 8192,
		serializeFn: func(keepRecentTokens int) (string, int, error) {
			return "history", 2, nil
		},
		oneShotFn: func(ctx context.Context, systemPrompt, input string) (string, error) {
			failCount++
			if failCount <= 2 {
				return "", errors.New("api error")
			}
			return "summary", nil
		},
	}

	orchestrator := New(provider, nil)

	// Fail twice
	for i := 0; i < 2; i++ {
		assert.True(t, orchestrator.TriggerNow(context.Background()))
		_ = orchestrator.WaitForPending(context.Background())
	}

	// Succeed once - should reset counter
	assert.True(t, orchestrator.TriggerNow(context.Background()))
	require.NoError(t, orchestrator.WaitForPending(context.Background()))

	orchestrator.mu.Lock()
	assert.Zero(t, orchestrator.consecutiveFailures)
	orchestrator.mu.Unlock()

	// TriggerIfNeeded should work again
	assert.True(t, orchestrator.TriggerIfNeeded(context.Background()))
	require.NoError(t, orchestrator.WaitForPending(context.Background()))
}

// --- Reactive Compact Tests ---

func TestTriggerReactiveOneShotGuard(t *testing.T) {
	t.Parallel()

	provider := &mockProvider{
		currentTokens:   190000,
		contextWindow:   200000,
		maxOutputTokens: 8192,
		serializeFn: func(keepRecentTokens int) (string, int, error) {
			return "history", 2, nil
		},
		oneShotFn: func(ctx context.Context, systemPrompt, input string) (string, error) {
			return "summary", nil
		},
	}

	orchestrator := New(provider, nil)

	// First call succeeds
	require.NoError(t, orchestrator.TriggerReactive(context.Background()))
	assert.Equal(t, 1, provider.replaceCalls)

	// Second call in same turn is blocked
	err := orchestrator.TriggerReactive(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already attempted")
	assert.Equal(t, 1, provider.replaceCalls) // no extra Replace call

	// After WaitForPending (new turn), guard resets
	require.NoError(t, orchestrator.WaitForPending(context.Background()))
	require.NoError(t, orchestrator.TriggerReactive(context.Background()))
	assert.Equal(t, 2, provider.replaceCalls)
}

func TestTriggerReactiveUsesCompressFastPath(t *testing.T) {
	t.Parallel()

	provider := &mockProvider{
		currentTokens:   190000,
		contextWindow:   200000,
		maxOutputTokens: 8192,
		compressFn: func(ctx context.Context, keepRecentTokens int) (int, error) {
			return 5, nil
		},
	}

	orchestrator := New(provider, nil)

	require.NoError(t, orchestrator.TriggerReactive(context.Background()))
	assert.Equal(t, 1, provider.compressCalls)
	assert.Zero(t, provider.serializeCalls)
	assert.Zero(t, provider.replaceCalls)
}
