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

func TestBackoff_Increasing(t *testing.T) {
	// Sample each attempt multiple times and use the mean to smooth jitter.
	mean := func(attempt int) time.Duration {
		var total time.Duration
		n := 50
		for i := 0; i < n; i++ {
			total += Backoff(attempt)
		}
		return total / time.Duration(n)
	}

	d0 := mean(0)
	d1 := mean(1)
	d2 := mean(2)
	d3 := mean(3)

	assert.Less(t, d0, d1, "attempt 0 mean should be less than attempt 1")
	assert.Less(t, d1, d2, "attempt 1 mean should be less than attempt 2")
	assert.Less(t, d2, d3, "attempt 2 mean should be less than attempt 3")
}

func TestBackoff_JitterSpread(t *testing.T) {
	attempt := 3
	// base = 200 * 2^3 = 1600ms, jitter range [0.9, 1.1) -> [1440ms, 1760ms)
	expectedBase := 1600 * time.Millisecond
	lowerBound := time.Duration(float64(expectedBase) * 0.9)
	upperBound := time.Duration(float64(expectedBase) * 1.1)

	for i := 0; i < 100; i++ {
		d := Backoff(attempt)
		assert.GreaterOrEqual(t, d, lowerBound, "call %d: duration %v should be >= %v", i, d, lowerBound)
		assert.Less(t, d, upperBound, "call %d: duration %v should be < %v", i, d, upperBound)
	}
}
