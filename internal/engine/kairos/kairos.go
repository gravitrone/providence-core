package kairos

import (
	"fmt"
	"sync"
	"time"
)

// State tracks the kairos autonomous loop state.
type State struct {
	Active     bool
	Paused     bool
	FocusState string // "focused" | "unfocused" | "unknown"
	LastTickAt time.Time
	TickCount  int
	// LastUserMsg tracks when the user last sent a message.
	// If no message in 5 minutes, the session is considered "unfocused".
	LastUserMsg time.Time
	mu          sync.Mutex
}

// FocusTimeout is how long without user input before we consider the
// session "unfocused" (autonomous mode).
const FocusTimeout = 5 * time.Minute

// New creates a new kairos state with defaults.
func New() *State {
	return &State{FocusState: "unknown"}
}

// Activate enables kairos mode.
func (s *State) Activate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Active = true
	s.Paused = false
}

// Deactivate disables kairos mode.
func (s *State) Deactivate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Active = false
}

// Pause temporarily stops tick injection.
func (s *State) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Paused = true
}

// Resume resumes tick injection after a pause.
func (s *State) Resume() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Paused = false
}

// ShouldTick returns true if a tick should be injected.
func (s *State) ShouldTick() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Active && !s.Paused
}

// RecordTick marks that a tick was injected.
func (s *State) RecordTick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastTickAt = time.Now()
	s.TickCount++
}

// RecordUserMessage updates the last user message timestamp and focus state.
func (s *State) RecordUserMessage() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastUserMsg = time.Now()
	s.FocusState = "focused"
}

// UpdateFocus refreshes the focus state based on time since last user message.
func (s *State) UpdateFocus() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.LastUserMsg.IsZero() {
		s.FocusState = "unknown"
		return
	}
	if time.Since(s.LastUserMsg) > FocusTimeout {
		s.FocusState = "unfocused"
	} else {
		s.FocusState = "focused"
	}
}

// Status returns a human-readable summary of the current kairos state.
func (s *State) Status() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	stateStr := "inactive"
	if s.Active && !s.Paused {
		stateStr = "active"
	} else if s.Active && s.Paused {
		stateStr = "paused"
	}

	lastTick := "never"
	if !s.LastTickAt.IsZero() {
		lastTick = s.LastTickAt.Format("3:04:05 PM")
	}

	return fmt.Sprintf("kairos: %s | focus: %s | ticks: %d | last tick: %s",
		stateStr, s.FocusState, s.TickCount, lastTick)
}

// GenerateTick creates the tick message content.
func GenerateTick() string {
	return fmt.Sprintf("<tick>%s</tick>", time.Now().Format("3:04:05 PM"))
}
