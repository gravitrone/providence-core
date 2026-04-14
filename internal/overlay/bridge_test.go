package overlay

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gravitrone/providence-core/internal/engine/ember"
	"github.com/gravitrone/providence-core/internal/engine/session"
)

// --- Fake engine ---

type fakeEngine struct {
	sendCalled   []string
	interrupted  bool
	model        string
	engineType   string
	bus          *session.Bus
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{
		model:      "sonnet",
		engineType: "claude",
		bus:        session.NewBus(),
	}
}

func (e *fakeEngine) Send(text string) error {
	e.sendCalled = append(e.sendCalled, text)
	return nil
}

func (e *fakeEngine) Interrupt() { e.interrupted = true }
func (e *fakeEngine) Model() string { return e.model }
func (e *fakeEngine) EngineType() string { return e.engineType }
func (e *fakeEngine) SessionBus() *session.Bus { return e.bus }

// --- Tests ---

func TestBridgeContextUpdateStoresPendingReminder(t *testing.T) {
	eng := newFakeEngine()
	em := ember.New()
	bridge := NewBridge(eng, em, nil, nil, nil)

	u := ContextUpdate{
		Timestamp:   time.Now(),
		ActiveApp:   "com.microsoft.VSCode",
		WindowTitle: "main.go",
		Activity:    "coding",
		AXSummary:   "func Send(text string) error { ... line 42",
		Transcript:  "what does this function do",
		ChangeKind:  "pattern",
	}

	err := bridge.OnContextUpdate(nil, u)
	require.NoError(t, err)

	reminder := bridge.PendingSystemReminder()
	assert.NotEmpty(t, reminder)
	assert.Contains(t, reminder, "<system-reminder>")
	assert.Contains(t, reminder, "</system-reminder>")
	assert.Contains(t, reminder, "com.microsoft.VSCode")
	assert.Contains(t, reminder, "coding")
	assert.Contains(t, reminder, "func Send")
	assert.Contains(t, reminder, "what does this function do")
}

func TestBridgePendingReminderClearsAfterRead(t *testing.T) {
	bridge := NewBridge(newFakeEngine(), ember.New(), nil, nil, nil)

	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		ActiveApp: "Safari",
		Activity:  "browsing",
		ChangeKind: "heartbeat",
	}))

	first := bridge.PendingSystemReminder()
	assert.NotEmpty(t, first)

	// Second read should return empty.
	second := bridge.PendingSystemReminder()
	assert.Empty(t, second)
}

func TestBridgeEmberRequestActive(t *testing.T) {
	em := ember.New()
	assert.False(t, em.Active)

	bridge := NewBridge(newFakeEngine(), em, nil, nil, nil)
	err := bridge.OnEmberRequest(nil, EmberRequest{Desired: "active"})
	require.NoError(t, err)
	assert.True(t, em.Active)
}

func TestBridgeEmberRequestInactive(t *testing.T) {
	em := ember.New()
	em.Activate()
	assert.True(t, em.Active)

	bridge := NewBridge(newFakeEngine(), em, nil, nil, nil)
	err := bridge.OnEmberRequest(nil, EmberRequest{Desired: "inactive"})
	require.NoError(t, err)
	assert.False(t, em.Active)
}

func TestBridgeEmberRequestPaused(t *testing.T) {
	em := ember.New()
	em.Activate()

	bridge := NewBridge(newFakeEngine(), em, nil, nil, nil)
	err := bridge.OnEmberRequest(nil, EmberRequest{Desired: "paused"})
	require.NoError(t, err)
	assert.True(t, em.Paused)
}

func TestBridgeEmberRequestResumed(t *testing.T) {
	em := ember.New()
	em.Activate()
	em.Pause()
	assert.True(t, em.Paused)

	bridge := NewBridge(newFakeEngine(), em, nil, nil, nil)
	err := bridge.OnEmberRequest(nil, EmberRequest{Desired: "resumed"})
	require.NoError(t, err)
	assert.False(t, em.Paused)
}

func TestBridgeEmberRequestUnknownDesired(t *testing.T) {
	bridge := NewBridge(newFakeEngine(), ember.New(), nil, nil, nil)
	err := bridge.OnEmberRequest(nil, EmberRequest{Desired: "turbo_mode"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "turbo_mode")
}

func TestBridgeUserQueryForwardsToEngine(t *testing.T) {
	eng := newFakeEngine()
	bridge := NewBridge(eng, ember.New(), nil, nil, nil)

	err := bridge.OnUserQuery(nil, UserQuery{Text: "hello world", Source: "wake_word"})
	require.NoError(t, err)
	require.Len(t, eng.sendCalled, 1)
	assert.Equal(t, "hello world", eng.sendCalled[0])
}

func TestBridgeInterruptForwardsToEngine(t *testing.T) {
	eng := newFakeEngine()
	bridge := NewBridge(eng, ember.New(), nil, nil, nil)

	err := bridge.OnInterrupt(nil)
	require.NoError(t, err)
	assert.True(t, eng.interrupted)
}

func TestBridgeOnHelloReturnsWelcome(t *testing.T) {
	eng := newFakeEngine()
	em := ember.New()
	em.Activate()

	bridge := NewBridge(eng, em, nil, nil, nil)
	bridge.SetSessionID("sess-abc")

	w := bridge.OnHello(nil, Hello{ClientVersion: "1.0", PID: 100})
	assert.Equal(t, "sess-abc", w.SessionID)
	assert.Equal(t, "claude", w.Engine)
	assert.Equal(t, "sonnet", w.Model)
	assert.True(t, w.EmberActive)
	assert.False(t, w.Timestamp.IsZero())
}

func TestBridgeOnUIEventNoError(t *testing.T) {
	bridge := NewBridge(newFakeEngine(), ember.New(), nil, nil, nil)
	err := bridge.OnUIEvent(nil, UIEvent{Kind: "dismiss"})
	assert.NoError(t, err)
}

func TestBridgeOnDisconnectNoError(t *testing.T) {
	bridge := NewBridge(newFakeEngine(), ember.New(), nil, nil, nil)
	// Must not panic.
	bridge.OnDisconnect(nil)
}

func TestBridgeNilEngineUserQueryReturnsError(t *testing.T) {
	bridge := NewBridge(nil, ember.New(), nil, nil, nil)
	err := bridge.OnUserQuery(nil, UserQuery{Text: "hi"})
	require.Error(t, err)
}

func TestBridgeNilEngineInterruptReturnsError(t *testing.T) {
	bridge := NewBridge(nil, ember.New(), nil, nil, nil)
	err := bridge.OnInterrupt(nil)
	require.Error(t, err)
}

func TestBridgeNilEmberEmberRequestReturnsError(t *testing.T) {
	bridge := NewBridge(newFakeEngine(), nil, nil, nil, nil)
	err := bridge.OnEmberRequest(nil, EmberRequest{Desired: "active"})
	require.Error(t, err)
}

// TestBridgeStartForwardsSessionEvents verifies that Start subscribes to the
// session bus and broadcasts events to connected clients.
func TestBridgeStartForwardsSessionEvents(t *testing.T) {
	eng := newFakeEngine()
	em := ember.New()

	// Start a real UDS server with the bridge as handler.
	spy := &spyHandler{}
	srv, cancel := startTestServer(t, spy)
	defer cancel()
	defer srv.Close()

	bridge := NewBridge(eng, em, srv, nil, nil)

	ctx, stopBridge := context.WithCancel(context.Background())
	defer stopBridge()
	go bridge.Start(ctx)

	// Connect a client.
	conn := dialTestClient(t, srv)
	sendEnvelope(t, conn, TypeHello, Hello{PID: 1})
	readEnvelope(t, conn) // welcome

	// Publish an event to the session bus.
	eng.bus.Publish(session.Event{Type: session.EventNewMessage, Data: "test payload"})

	// The bridge should forward it as a session_event to all clients.
	env := readEnvelope(t, conn)
	assert.Equal(t, TypeSessionEvent, env.Type)

	var se SessionEvent
	require.NoError(t, json.Unmarshal(env.Data, &se))
	assert.Equal(t, session.EventNewMessage, se.Type)
}

// TestFormatContextReminder checks the formatting of various ContextUpdate states.
func TestFormatContextReminder(t *testing.T) {
	cases := []struct {
		name        string
		u           ContextUpdate
		wantContain []string
	}{
		{
			name: "full context with ax and transcript",
			u: ContextUpdate{
				Timestamp:   time.Date(2026, 4, 14, 14, 32, 10, 0, time.UTC),
				ActiveApp:   "VS Code",
				WindowTitle: "main.go",
				Activity:    "coding",
				AXSummary:   "Editor: func Send",
				Transcript:  "what's this function",
				ChangeKind:  "pattern",
			},
			wantContain: []string{
				"<system-reminder>",
				"</system-reminder>",
				"14:32:10",
				"VS Code",  // ActiveApp
				"main.go",  // WindowTitle
				"coding",
				"Editor: func Send",
				"what's this function",
			},
		},
		{
			name: "idle with no speech",
			u: ContextUpdate{
				ActiveApp:  "Finder",
				Activity:   "idle",
				ChangeKind: "heartbeat",
			},
			wantContain: []string{
				"<system-reminder>",
				"Finder",
				"idle",
				"(silent)",
			},
		},
		{
			name: "missing activity defaults to general",
			u: ContextUpdate{
				ActiveApp:  "Terminal",
				ChangeKind: "pattern",
			},
			wantContain: []string{"general"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := formatContextReminder(tc.u)
			for _, want := range tc.wantContain {
				assert.True(t, strings.Contains(out, want),
					"expected %q to contain %q\ngot: %s", tc.name, want, out)
			}
		})
	}
}

// TestBridgeSatisfiesInjector verifies Bridge implements the Injector interface.
func TestBridgeSatisfiesInjector(t *testing.T) {
	var _ Injector = (*Bridge)(nil)
}

