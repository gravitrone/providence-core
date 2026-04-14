package ember

import (
	"regexp"
	"strings"
	"sync"
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

func TestEmber_ActivatePauseResumeRoundtrip(t *testing.T) {
	s := New()
	s.Activate()
	require.True(t, s.Active)
	require.False(t, s.Paused)

	s.Pause()
	require.True(t, s.Active)
	require.True(t, s.Paused)
	require.False(t, s.ShouldTick(), "paused state must not tick")

	s.Resume()
	require.True(t, s.Active)
	require.False(t, s.Paused)
	require.True(t, s.ShouldTick(), "resumed state must tick")
}

func TestEmber_DeactivateFromPaused(t *testing.T) {
	s := New()
	s.Activate()
	s.Pause()
	s.Deactivate()
	assert.False(t, s.ShouldTick(), "deactivated-from-paused must not tick")
	assert.False(t, s.Active)
}

func TestEmber_FocusTimeoutTransitionsUnfocused(t *testing.T) {
	s := New()
	s.RecordUserMessage()
	assert.Equal(t, "focused", s.FocusState)

	// backdate LastUserMsg past FocusTimeout
	s.mu.Lock()
	s.LastUserMsg = time.Now().Add(-(FocusTimeout + time.Second))
	s.mu.Unlock()

	s.UpdateFocus()
	assert.Equal(t, "unfocused", s.FocusState)
}

func TestEmber_ConcurrentActivateSafe(t *testing.T) {
	s := New()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Activate()
		}()
	}
	wg.Wait()
	// all goroutines called Activate - state must be consistent (no race)
	assert.True(t, s.Active)
	assert.False(t, s.Paused)
}

func TestEmber_GenerateTickFormat(t *testing.T) {
	// GenerateTick uses time.Format("3:04:05 PM") - 12h clock, no leading zero for hour
	// valid examples: "<tick>1:02:03 AM</tick>", "<tick>12:59:59 PM</tick>"
	re := regexp.MustCompile(`^<tick>\d{1,2}:\d{2}:\d{2} (AM|PM)</tick>$`)
	tick := GenerateTick()
	assert.Regexp(t, re, tick, "tick format must match <tick>H:MM:SS AM/PM</tick>")
}

func TestEmber_TickCountMonotonicOnRecordTick(t *testing.T) {
	s := New()
	const n = 7
	for i := 0; i < n; i++ {
		s.RecordTick()
	}
	assert.Equal(t, n, s.TickCount, "tick count must increment by 1 per RecordTick call")
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
