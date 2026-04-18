package compact

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMemoryAugmentedSummaryNoMemoryReturnsSummary(t *testing.T) {
	t.Parallel()

	got := buildMemoryAugmentedSummary("", "plain summary")
	assert.Equal(t, "plain summary", got)
}

func TestBuildMemoryAugmentedSummaryWrapsMemoryAndSummary(t *testing.T) {
	t.Parallel()

	got := buildMemoryAugmentedSummary("memory body", "summary body")

	require.True(t, strings.Contains(got, "<session-memory>"))
	require.True(t, strings.Contains(got, "</session-memory>"))
	require.True(t, strings.Contains(got, "memory body"))
	require.True(t, strings.Contains(got, "summary body"))

	// Memory MUST appear before the summary so the restored context treats
	// it as the authoritative prefix.
	memIdx := strings.Index(got, "memory body")
	sumIdx := strings.Index(got, "summary body")
	require.True(t, memIdx >= 0 && sumIdx >= 0)
	assert.Less(t, memIdx, sumIdx, "memory must appear before summary")
}

func TestBuildMemoryAugmentedSummaryMemoryOnly(t *testing.T) {
	t.Parallel()

	got := buildMemoryAugmentedSummary("just memory", "")
	assert.Contains(t, got, "<session-memory>")
	assert.Contains(t, got, "just memory")
	assert.NotContains(t, got, "<context-summary>")
}

// TestCompactorReadsMemoryBeforeFallingBackToHistory verifies that the
// orchestrator injects memory as the authoritative prefix of the replacement
// payload on the async path. The mock captures what the provider's Replace
// receives so we can assert memory appeared first.
func TestCompactorReadsMemoryBeforeFallingBackToHistory(t *testing.T) {
	t.Parallel()

	var captured atomic.Value
	provider := &mockProvider{
		currentTokens:   1,
		contextWindow:   1000,
		maxOutputTokens: 100,
		serializeFn: func(int) (string, int, error) {
			return "raw history body", 2, nil
		},
		oneShotFn: func(context.Context, string, string) (string, error) {
			return "summary from history", nil
		},
		replaceFn: func(summary string, cutIndex int) error {
			captured.Store(summary)
			return nil
		},
	}

	o := New(provider, nil)
	o.SetMemoryReader(func() (string, error) {
		return "authoritative memory body", nil
	})

	require.True(t, o.TriggerNow(context.Background()))
	require.NoError(t, o.WaitForPending(context.Background()))

	got, ok := captured.Load().(string)
	require.True(t, ok, "replace must have been called")

	require.Contains(t, got, "authoritative memory body")
	require.Contains(t, got, "summary from history")
	require.Contains(t, got, "<session-memory>")

	memIdx := strings.Index(got, "authoritative memory body")
	sumIdx := strings.Index(got, "summary from history")
	assert.Less(t, memIdx, sumIdx)
	assert.Equal(t, 1, provider.replaceCalls)
}

// TestCompactorReactivePathReadsMemoryBeforeFallback covers the reactive
// (synchronous 413 recovery) path, mirroring the async-path guarantee.
func TestCompactorReactivePathReadsMemoryBeforeFallback(t *testing.T) {
	t.Parallel()

	var captured atomic.Value
	provider := &mockProvider{
		contextWindow:   1000,
		maxOutputTokens: 100,
		serializeFn: func(int) (string, int, error) {
			return "reactive raw body", 1, nil
		},
		oneShotFn: func(context.Context, string, string) (string, error) {
			return "reactive summary", nil
		},
		replaceFn: func(summary string, cutIndex int) error {
			captured.Store(summary)
			return nil
		},
	}

	o := New(provider, nil)
	o.SetMemoryReader(func() (string, error) {
		return "reactive memory", nil
	})

	require.NoError(t, o.TriggerReactive(context.Background()))

	got, ok := captured.Load().(string)
	require.True(t, ok)
	assert.Contains(t, got, "reactive memory")
	assert.Contains(t, got, "reactive summary")
	memIdx := strings.Index(got, "reactive memory")
	sumIdx := strings.Index(got, "reactive summary")
	assert.Less(t, memIdx, sumIdx)
}

// TestCompactorMemoryReadErrorFallsThroughCleanly asserts that a reader that
// errors does not abort compaction. The provider's Replace still receives the
// plain summary and no memory envelope.
func TestCompactorMemoryReadErrorFallsThroughCleanly(t *testing.T) {
	t.Parallel()

	var captured atomic.Value
	provider := &mockProvider{
		contextWindow:   1000,
		maxOutputTokens: 100,
		serializeFn: func(int) (string, int, error) {
			return "raw body", 1, nil
		},
		oneShotFn: func(context.Context, string, string) (string, error) {
			return "summary only", nil
		},
		replaceFn: func(summary string, cutIndex int) error {
			captured.Store(summary)
			return nil
		},
	}

	o := New(provider, nil)
	o.SetMemoryReader(func() (string, error) {
		return "", errors.New("reader blew up")
	})

	require.True(t, o.TriggerNow(context.Background()))
	require.NoError(t, o.WaitForPending(context.Background()))

	got, ok := captured.Load().(string)
	require.True(t, ok)
	assert.Equal(t, "summary only", got)
	assert.NotContains(t, got, "<session-memory>")
}

// TestCompactorWithNoReaderLeavesSummaryAlone is the baseline: with no memory
// reader configured the pre-existing behaviour is preserved.
func TestCompactorWithNoReaderLeavesSummaryAlone(t *testing.T) {
	t.Parallel()

	var captured atomic.Value
	provider := &mockProvider{
		contextWindow:   1000,
		maxOutputTokens: 100,
		serializeFn: func(int) (string, int, error) {
			return "raw body", 1, nil
		},
		oneShotFn: func(context.Context, string, string) (string, error) {
			return "just summary", nil
		},
		replaceFn: func(summary string, cutIndex int) error {
			captured.Store(summary)
			return nil
		},
	}

	o := New(provider, nil)

	require.True(t, o.TriggerNow(context.Background()))
	require.NoError(t, o.WaitForPending(context.Background()))

	got, ok := captured.Load().(string)
	require.True(t, ok)
	assert.Equal(t, "just summary", got)
}
