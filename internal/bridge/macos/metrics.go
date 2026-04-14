//go:build darwin

package macos

import (
	"sort"
	"sync"
	"time"
)

const metricsBucketSize = 1024

// bucket holds a ring buffer of latency samples plus error/call counts.
type bucket struct {
	count    int64
	errCount int64
	samples  [metricsBucketSize]time.Duration
	head     int
	filled   bool // true once the ring buffer has wrapped around at least once
}

func (b *bucket) record(dur time.Duration, ok bool) {
	b.count++
	if !ok {
		b.errCount++
	}
	b.samples[b.head] = dur
	b.head++
	if b.head >= metricsBucketSize {
		b.head = 0
		b.filled = true
	}
}

// snapshot returns a sorted copy of the active samples.
func (b *bucket) snapshot() []time.Duration {
	n := b.head
	if b.filled {
		n = metricsBucketSize
	}
	if n == 0 {
		return nil
	}
	out := make([]time.Duration, n)
	copy(out, b.samples[:n])
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// percentile returns the value at the given percentile (0-100) of sorted samples.
func percentile(sorted []time.Duration, pct float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * pct / 100.0)
	return sorted[idx]
}

// HistogramSnapshot is a read-only snapshot of one op's metrics.
type HistogramSnapshot struct {
	Op         string        `json:"op"`
	Count      int64         `json:"count"`
	ErrorCount int64         `json:"error_count"`
	P50        time.Duration `json:"p50_ns"`
	P95        time.Duration `json:"p95_ns"`
	P99        time.Duration `json:"p99_ns"`
	Max        time.Duration `json:"max_ns"`
}

// Metrics tracks per-op latency and success/failure counts for the bridge.
type Metrics struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

// NewMetrics creates a new Metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{
		buckets: make(map[string]*bucket),
	}
}

// Record records a single op's duration and ok/err outcome.
func (m *Metrics) Record(op string, dur time.Duration, ok bool) {
	m.mu.Lock()
	b, exists := m.buckets[op]
	if !exists {
		b = &bucket{}
		m.buckets[op] = b
	}
	b.record(dur, ok)
	m.mu.Unlock()
}

// Snapshot returns p50/p95/p99/max and counts per op.
func (m *Metrics) Snapshot() map[string]HistogramSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make(map[string]HistogramSnapshot, len(m.buckets))
	for op, b := range m.buckets {
		sorted := b.snapshot()
		snap := HistogramSnapshot{
			Op:         op,
			Count:      b.count,
			ErrorCount: b.errCount,
		}
		if len(sorted) > 0 {
			snap.P50 = percentile(sorted, 50)
			snap.P95 = percentile(sorted, 95)
			snap.P99 = percentile(sorted, 99)
			snap.Max = sorted[len(sorted)-1]
		}
		out[op] = snap
	}
	return out
}
