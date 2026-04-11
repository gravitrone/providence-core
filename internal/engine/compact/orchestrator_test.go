package compact

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProvider struct {
	currentTokens int
	contextWindow int

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

func TestTriggerIfNeededBelowThreshold_NoOp(t *testing.T) {
	t.Parallel()

	provider := &mockProvider{
		currentTokens: 799,
		contextWindow: 1000,
	}

	orchestrator := New(provider, nil)

	assert.False(t, orchestrator.TriggerIfNeeded(context.Background()))
	assert.False(t, orchestrator.IsRunning())
	assert.Zero(t, provider.compressCalls)
}

func TestTriggerIfNeededAboveThreshold_StartsAsync(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	provider := &mockProvider{
		currentTokens: 800,
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
