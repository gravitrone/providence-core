package engine

import (
	"math"
	"math/rand"
	"time"
)

const (
	// BackoffInitialMS is the base delay in milliseconds for the first retry.
	BackoffInitialMS = 200

	// BackoffFactor is the exponential multiplier per retry attempt.
	BackoffFactor = 2.0
)

// Backoff returns the delay for the given retry attempt (0-indexed).
// Uses exponential backoff with +/-10% jitter to prevent thundering herd.
func Backoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	exp := math.Pow(BackoffFactor, float64(attempt))
	baseMS := float64(BackoffInitialMS) * exp
	jitter := 0.9 + rand.Float64()*0.2 // [0.9, 1.1)
	return time.Duration(baseMS*jitter) * time.Millisecond
}
