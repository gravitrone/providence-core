package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gravitrone/providence-core/internal/config"
)

// TestEscInterruptsInflightTurn verifies that pressing Escape while a
// turn is streaming calls engine.Interrupt and surfaces the "Interrupted."
// system message. The test reuses the emberTestEngine stub from the
// /ember activation tests for its Interrupt tracking.
func TestEscInterruptsInflightTurn(t *testing.T) {
	at, eng := newEmberTestAgentTab(t, config.OverlayConfig{Spawn: emberBoolPtr(true)})
	at.streaming = true

	updated, _ := at.handleKey(keyPress("esc"))

	assert.Equal(t, int32(1), eng.InterruptCount(), "Escape must call engine.Interrupt exactly once")

	// The system message is appended to the updated AgentTab's message
	// list, not the receiver copy (handleKey has a value receiver).
	found := false
	for _, m := range updated.messages {
		if m.Role == "system" && m.Content == "Interrupted." {
			found = true
			break
		}
	}
	assert.True(t, found, "Escape must append an 'Interrupted.' system message")
}

// TestEscIsNoOpWhenNoTurnInflight verifies the guard path: when the
// session is idle, Escape must not call Interrupt and must not add a
// stray system message. Other Escape handlers (e.g. queue-cursor close)
// should still receive the key untouched.
func TestEscIsNoOpWhenNoTurnInflight(t *testing.T) {
	at, eng := newEmberTestAgentTab(t, config.OverlayConfig{Spawn: emberBoolPtr(true)})
	at.streaming = false

	updated, _ := at.handleKey(keyPress("esc"))

	assert.Zero(t, eng.InterruptCount(), "idle Escape must not call engine.Interrupt")
	for _, m := range updated.messages {
		assert.NotEqual(t, "Interrupted.", m.Content, "idle Escape must not emit the Interrupted system message")
	}
}

// TestEscQueueCursorCloseTakesPrecedence verifies that when the user
// opened the queue review (queueCursor >= 0) AND a turn is streaming,
// Escape still closes the queue review first. Interrupt fires only on a
// second press after the overlay is dismissed.
func TestEscQueueCursorCloseTakesPrecedence(t *testing.T) {
	at, eng := newEmberTestAgentTab(t, config.OverlayConfig{Spawn: emberBoolPtr(true)})
	at.streaming = true
	at.queueCursor = 0

	updated, _ := at.handleKey(keyPress("esc"))
	assert.Equal(t, -1, updated.queueCursor, "Escape must close the queue review")
	assert.Zero(t, eng.InterruptCount(), "Escape must not also interrupt while closing an overlay")

	// Second Escape with cursor already closed and turn still in flight
	// should now route to interrupt.
	updated2, _ := updated.handleKey(keyPress("esc"))
	assert.Equal(t, int32(1), eng.InterruptCount(), "second Escape must interrupt once the overlay is closed")
	_ = updated2
}

// TestEscDoesNotPanicWhenEngineNil verifies the nil-engine guard inside
// the Escape branch. A mid-construction AgentTab (engine not yet wired)
// must not crash if the user hits Escape.
func TestEscDoesNotPanicWhenEngineNil(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil, nil)
	at.streaming = true
	// Deliberately leave at.engine == nil.

	require.NotPanics(t, func() {
		_, _ = at.handleKey(keyPress("esc"))
	})
}
