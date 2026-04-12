package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSleepToolMetadata(t *testing.T) {
	s := SleepTool{}
	assert.Equal(t, "Sleep", s.Name())
	assert.True(t, s.ReadOnly())
	assert.NotEmpty(t, s.Description())

	schema := s.InputSchema()
	require.NotNil(t, schema)
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	_, hasDuration := props["duration_ms"]
	assert.True(t, hasDuration)
}

func TestSleepToolDuration(t *testing.T) {
	s := SleepTool{}

	start := time.Now()
	result := s.Execute(context.Background(), map[string]any{
		"duration_ms": 200,
	})
	elapsed := time.Since(start)

	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "Slept for")
	// Should have slept at least 150ms (some tolerance).
	assert.True(t, elapsed >= 150*time.Millisecond, "sleep was too short: %v", elapsed)
	// And not more than 1s.
	assert.True(t, elapsed < 1*time.Second, "sleep was too long: %v", elapsed)
}

func TestSleepToolInterrupt(t *testing.T) {
	s := SleepTool{}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after 100ms, but ask for 5s sleep.
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	result := s.Execute(ctx, map[string]any{
		"duration_ms": 5000,
	})
	elapsed := time.Since(start)

	assert.False(t, result.IsError)
	assert.True(t, strings.Contains(result.Content, "interrupted"), "expected interrupt message, got: %s", result.Content)
	// Should have been interrupted well before 5s.
	assert.True(t, elapsed < 1*time.Second, "should have been interrupted quickly, took: %v", elapsed)
}

func TestSleepToolClampMin(t *testing.T) {
	s := SleepTool{}

	start := time.Now()
	result := s.Execute(context.Background(), map[string]any{
		"duration_ms": 10, // below minimum, should clamp to 100ms
	})
	elapsed := time.Since(start)

	assert.False(t, result.IsError)
	assert.True(t, elapsed >= 90*time.Millisecond, "clamp to minimum didn't work: %v", elapsed)
}
