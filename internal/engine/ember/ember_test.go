package ember

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActivateDeactivate(t *testing.T) {
	s := New()
	assert.False(t, s.Active)

	s.Activate()
	assert.True(t, s.Active)
	assert.False(t, s.Paused)

	s.Deactivate()
	assert.False(t, s.Active)
}

func TestPauseResume(t *testing.T) {
	s := New()
	s.Activate()

	s.Pause()
	assert.True(t, s.Paused)
	assert.True(t, s.Active) // still active, just paused

	s.Resume()
	assert.False(t, s.Paused)
}

func TestShouldTick(t *testing.T) {
	tests := []struct {
		name     string
		active   bool
		paused   bool
		expected bool
	}{
		{"active and unpaused", true, false, true},
		{"active but paused", true, true, false},
		{"inactive and unpaused", false, false, false},
		{"inactive and paused", false, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			s.Active = tt.active
			s.Paused = tt.paused
			assert.Equal(t, tt.expected, s.ShouldTick())
		})
	}
}

func TestRecordTick(t *testing.T) {
	s := New()
	assert.Equal(t, 0, s.TickCount)
	assert.True(t, s.LastTickAt.IsZero())

	s.RecordTick()
	assert.Equal(t, 1, s.TickCount)
	assert.False(t, s.LastTickAt.IsZero())

	s.RecordTick()
	assert.Equal(t, 2, s.TickCount)
}

func TestGenerateTick(t *testing.T) {
	tick := GenerateTick()
	require.True(t, strings.HasPrefix(tick, "<tick>"), "tick should start with <tick>")
	require.True(t, strings.HasSuffix(tick, "</tick>"), "tick should end with </tick>")
}

func TestFocusDetection(t *testing.T) {
	s := New()
	assert.Equal(t, "unknown", s.FocusState)

	// User sends a message - should become focused.
	s.RecordUserMessage()
	assert.Equal(t, "focused", s.FocusState)

	// Simulate time passing beyond the focus timeout.
	s.mu.Lock()
	s.LastUserMsg = time.Now().Add(-6 * time.Minute)
	s.mu.Unlock()

	s.UpdateFocus()
	assert.Equal(t, "unfocused", s.FocusState)
}

func TestStatus(t *testing.T) {
	s := New()
	status := s.Status()
	assert.Contains(t, status, "inactive")
	assert.Contains(t, status, "ticks: 0")

	s.Activate()
	status = s.Status()
	assert.Contains(t, status, "active")

	s.Pause()
	status = s.Status()
	assert.Contains(t, status, "paused")
}
