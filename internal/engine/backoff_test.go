package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBackoff_Attempt0(t *testing.T) {
	d := Backoff(0)
	// ~200ms +/- 10% -> [180ms, 220ms)
	assert.GreaterOrEqual(t, d, 180*time.Millisecond, "attempt 0 should be >= 180ms")
	assert.Less(t, d, 220*time.Millisecond, "attempt 0 should be < 220ms")
}

func TestBackoff_Attempt1(t *testing.T) {
	d := Backoff(1)
	// ~400ms +/- 10% -> [360ms, 440ms)
	assert.GreaterOrEqual(t, d, 360*time.Millisecond, "attempt 1 should be >= 360ms")
	assert.Less(t, d, 440*time.Millisecond, "attempt 1 should be < 440ms")
}

func TestBackoff_Attempt5(t *testing.T) {
	d := Backoff(5)
	// 200 * 2^5 = 6400ms +/- 10% -> [5760ms, 7040ms)
	assert.GreaterOrEqual(t, d, 5760*time.Millisecond, "attempt 5 should be >= 5760ms")
	assert.Less(t, d, 7040*time.Millisecond, "attempt 5 should be < 7040ms")
}

func TestBackoff_NegativeAttempt(t *testing.T) {
	d := Backoff(-1)
	// Clamped to attempt 0: ~200ms +/- 10%
	assert.GreaterOrEqual(t, d, 180*time.Millisecond, "negative attempt should clamp to 0")
	assert.Less(t, d, 220*time.Millisecond, "negative attempt should be < 220ms")
}

func TestBackoff_JitterProducesDifferentValues(t *testing.T) {
	seen := make(map[time.Duration]bool)
	for i := 0; i < 20; i++ {
		d := Backoff(0)
		seen[d] = true
	}
	// With jitter producing float values, 20 calls should yield at least 2
	// distinct durations. In practice it'll be many more.
	assert.Greater(t, len(seen), 1, "jitter should produce varying durations")
}
