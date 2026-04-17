package ui

import (
	"context"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gravitrone/providence-core/internal/config"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/session"
)

// closeCountingEngine implements engine.Engine and records how many times
// Close was invoked. It is intentionally minimal: the /clear handler only
// needs Close to fire, the rest of the surface is no-op so the interface is
// satisfied.
type closeCountingEngine struct {
	closeCount int32
	events     chan engine.ParsedEvent
}

func newCloseCountingEngine() *closeCountingEngine {
	return &closeCountingEngine{events: make(chan engine.ParsedEvent, 1)}
}

func (e *closeCountingEngine) Send(_ string) error                          { return nil }
func (e *closeCountingEngine) Events() <-chan engine.ParsedEvent            { return e.events }
func (e *closeCountingEngine) RespondPermission(_, _ string) error          { return nil }
func (e *closeCountingEngine) Interrupt()                                   {}
func (e *closeCountingEngine) Cancel()                                      {}
func (e *closeCountingEngine) Close()                                       { atomic.AddInt32(&e.closeCount, 1) }
func (e *closeCountingEngine) Status() engine.SessionStatus                 { return engine.StatusIdle }
func (e *closeCountingEngine) RestoreHistory(_ []engine.RestoredMessage) error { return nil }
func (e *closeCountingEngine) TriggerCompact(_ context.Context) error       { return nil }
func (e *closeCountingEngine) SessionBus() *session.Bus                     { return session.NewBus() }

func (e *closeCountingEngine) CloseCount() int32 {
	return atomic.LoadInt32(&e.closeCount)
}

// TestSlashClearClosesEngineAndResetsState guards F-013: /clear used to drop
// UI/store state but leave at.engine alive, so the next user prompt continued
// the previous model session with all the hidden conversation history. The
// fix routes both /clear and ctrl+l through clearSessionState which closes
// the engine, nils the pointer, and tears down session-correlated state.
func TestSlashClearClosesEngineAndResetsState(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil, nil)
	eng := newCloseCountingEngine()
	at.engine = eng

	// Seed state that should die with the engine.
	at.sessionID = "sess-xyz"
	at.messages = []ChatMessage{{Role: "user", Content: "hi", Done: true}}
	at.streamBuffer = "partial"
	at.toolInputBuffer = "{partial"
	at.pendingPerm = &engine.PermissionRequestEvent{QuestionID: "q-1"}
	at.streaming = true
	at.compacting = true
	at.currentTokens = 12345
	at.thinkingActive = true
	at.thinkingBuffer = "thinking..."
	at.pendingPortableState = &engine.ConversationState{Model: "sonnet"}

	handled, _ := at.handleSlashCommand("/clear")
	require.True(t, handled, "/clear should be handled by handleSlashCommand")

	assert.Equal(t, int32(1), eng.CloseCount(), "engine.Close must fire exactly once on /clear")
	assert.Nil(t, at.engine, "at.engine must be nil after /clear so stale reads cannot occur")
	assert.Empty(t, at.sessionID, "session id must clear")
	assert.Empty(t, at.messages, "ui messages must clear")
	assert.Empty(t, at.streamBuffer, "stream buffer must clear")
	assert.Empty(t, at.toolInputBuffer, "tool input buffer must clear")
	assert.Nil(t, at.pendingPerm, "pending permission must clear")
	assert.False(t, at.streaming, "streaming flag must reset")
	assert.False(t, at.compacting, "compacting flag must reset")
	assert.Equal(t, 0, at.currentTokens, "current tokens must reset")
	assert.False(t, at.thinkingActive, "thinking flag must reset")
	assert.Empty(t, at.thinkingBuffer, "thinking buffer must clear")
	assert.Nil(t, at.pendingPortableState, "pending portable state must drop")
}

// TestCtrlLClearClosesEngineAndResetsState mirrors the /clear regression for
// the ctrl+l keybinding which routes through handleKey, not handleSlashCommand.
// The streaming guard means we drive it from the idle state, and we cannot
// pre-seed pendingPerm because handleKey gives the permission prompt priority
// over every other key when one is open.
func TestCtrlLClearClosesEngineAndResetsState(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil, nil)
	at.Resize(120, 40)
	eng := newCloseCountingEngine()
	at.engine = eng

	at.sessionID = "sess-abc"
	at.messages = []ChatMessage{{Role: "user", Content: "hi", Done: true}}
	at.streamBuffer = "buf"
	at.currentTokens = 999
	// streaming must stay false: the ctrl+l handler short-circuits while
	// a turn is in flight to avoid clobbering an active engine session.
	at.streaming = false

	at, _ = at.handleKey(tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl})

	assert.Equal(t, int32(1), eng.CloseCount(), "engine.Close must fire exactly once on ctrl+l")
	assert.Nil(t, at.engine, "at.engine must be nil after ctrl+l")
	assert.Empty(t, at.sessionID)
	assert.Empty(t, at.messages)
	assert.Empty(t, at.streamBuffer)
	assert.Equal(t, 0, at.currentTokens)
}

// TestCtrlLClearWhileStreamingNoOps verifies the existing streaming guard:
// ctrl+l must not tear the engine down mid-turn. The user has to interrupt
// or wait first; otherwise an active stream would explode on the next event.
func TestCtrlLClearWhileStreamingNoOps(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil, nil)
	at.Resize(120, 40)
	eng := newCloseCountingEngine()
	at.engine = eng
	at.streaming = true
	at.messages = []ChatMessage{{Role: "user", Content: "hi", Done: true}}

	at, _ = at.handleKey(tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl})

	assert.Equal(t, int32(0), eng.CloseCount(), "ctrl+l must not close engine while streaming")
	assert.NotNil(t, at.engine, "engine must survive ctrl+l during stream")
	assert.NotEmpty(t, at.messages, "messages must survive ctrl+l during stream")
}

// TestSlashClearWithNilEngineIsSafe verifies the no-engine case (cleared chat
// before any send) does not panic and still scrubs the UI/session state.
func TestSlashClearWithNilEngineIsSafe(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil, nil)
	require.Nil(t, at.engine, "precondition: engine should be nil before any send")

	at.sessionID = "sess-abc"
	at.messages = []ChatMessage{{Role: "system", Content: "boot"}}

	handled, _ := at.handleSlashCommand("/clear")
	require.True(t, handled)

	assert.Nil(t, at.engine)
	assert.Empty(t, at.sessionID)
	assert.Empty(t, at.messages)
}

// TestClearSessionStateIsIdempotent confirms calling clearSessionState twice
// in a row does not double-close the engine (it was already nilled) and does
// not leave any partial state behind. This guards against future callers that
// might invoke the helper from multiple paths during a single tear-down.
func TestClearSessionStateIsIdempotent(t *testing.T) {
	at := NewAgentTab("", config.Config{}, nil, nil)
	eng := newCloseCountingEngine()
	at.engine = eng
	at.sessionID = "sess-1"
	at.messages = []ChatMessage{{Role: "user", Content: "hi"}}

	at.clearSessionState()
	at.clearSessionState()

	assert.Equal(t, int32(1), eng.CloseCount(), "second clear must not re-close the same engine")
	assert.Nil(t, at.engine)
	assert.Empty(t, at.sessionID)
	assert.Empty(t, at.messages)
}

// Compile-time guarantee that closeCountingEngine satisfies engine.Engine so
// the test does not silently fall out of sync with future interface additions.
var _ engine.Engine = (*closeCountingEngine)(nil)
