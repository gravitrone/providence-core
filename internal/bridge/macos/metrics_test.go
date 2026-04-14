//go:build darwin

package macos

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetrics_RecordAndSnapshot_Empty(t *testing.T) {
	m := NewMetrics()
	snap := m.Snapshot()
	assert.Empty(t, snap, "empty metrics should return empty snapshot")
}

func TestMetrics_RecordAndSnapshot_KnownDistribution(t *testing.T) {
	m := NewMetrics()

	// Record 100 samples: 0ms, 1ms, 2ms, ..., 99ms.
	// For a uniform 0-99ms distribution (100 values):
	//   p50 index = int(99 * 0.50) = 49  -> 49ms
	//   p95 index = int(99 * 0.95) = 94  -> 94ms
	//   p99 index = int(99 * 0.99) = 98  -> 98ms
	//   max = 99ms
	for i := 0; i < 100; i++ {
		m.Record("test_op", time.Duration(i)*time.Millisecond, true)
	}

	snap := m.Snapshot()
	require.Contains(t, snap, "test_op", "snapshot must contain recorded op")

	s := snap["test_op"]
	assert.Equal(t, int64(100), s.Count)
	assert.Equal(t, int64(0), s.ErrorCount)
	assert.Equal(t, 49*time.Millisecond, s.P50)
	assert.Equal(t, 94*time.Millisecond, s.P95)
	assert.Equal(t, 98*time.Millisecond, s.P99)
	assert.Equal(t, 99*time.Millisecond, s.Max)
}

func TestMetrics_RecordAndSnapshot_ErrorCount(t *testing.T) {
	m := NewMetrics()

	for i := 0; i < 10; i++ {
		ok := i%2 == 0 // 5 ok, 5 err
		m.Record("op", time.Millisecond, ok)
	}

	snap := m.Snapshot()
	require.Contains(t, snap, "op")
	s := snap["op"]
	assert.Equal(t, int64(10), s.Count)
	assert.Equal(t, int64(5), s.ErrorCount)
}

func TestMetrics_RingBuffer_Wraps(t *testing.T) {
	m := NewMetrics()

	// Record more than the ring buffer size to verify wrap-around.
	// After filling 1024 entries we add more; only the last 1024 should
	// be counted in percentiles, but Count tracks every call.
	total := metricsBucketSize + 50
	for i := 0; i < total; i++ {
		m.Record("op", time.Duration(i)*time.Microsecond, true)
	}

	snap := m.Snapshot()
	require.Contains(t, snap, "op")
	s := snap["op"]
	assert.Equal(t, int64(total), s.Count, "Count must track every call, not just ring buffer size")
	// Percentiles are computed over at most 1024 samples.
	assert.Greater(t, s.Max, time.Duration(0))
}

func TestMetrics_MultipleOps(t *testing.T) {
	m := NewMetrics()

	m.Record("screenshot", 10*time.Millisecond, true)
	m.Record("click", 5*time.Millisecond, false)
	m.Record("screenshot", 20*time.Millisecond, true)

	snap := m.Snapshot()
	assert.Len(t, snap, 2, "two ops should produce two snapshot entries")
	assert.Contains(t, snap, "screenshot")
	assert.Contains(t, snap, "click")

	clickSnap := snap["click"]
	assert.Equal(t, int64(1), clickSnap.Count)
	assert.Equal(t, int64(1), clickSnap.ErrorCount)
}

func TestMetrics_OpField(t *testing.T) {
	m := NewMetrics()
	m.Record("ax_tree", time.Millisecond, true)
	snap := m.Snapshot()
	s := snap["ax_tree"]
	assert.Equal(t, "ax_tree", s.Op)
}
