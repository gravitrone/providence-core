package overlay

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gravitrone/providence-core/internal/engine/ember"
	"github.com/gravitrone/providence-core/internal/engine/session"
)

// --- Fake engine ---

type fakeEngine struct {
	mu          sync.Mutex
	sendCalled  []string
	interrupted bool
	model       string
	engineType  string
	bus         *session.Bus
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{
		model:      "sonnet",
		engineType: "claude",
		bus:        session.NewBus(),
	}
}

func (e *fakeEngine) Send(text string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sendCalled = append(e.sendCalled, text)
	return nil
}

func (e *fakeEngine) sendCallCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.sendCalled)
}

func (e *fakeEngine) sendCallAt(i int) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sendCalled[i]
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
	assert.Contains(t, reminder, `<system-reminder origin="overlay">`)
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
				`<system-reminder origin="overlay">`,
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
				`<system-reminder origin="overlay">`,
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

// TestFormatContextReminderOriginAttribute verifies the origin="overlay" attribute
// is present in every formatted reminder for loopback suppression.
func TestFormatContextReminderOriginAttribute(t *testing.T) {
	u := ContextUpdate{
		ActiveApp:  "Safari",
		Activity:   "browsing",
		ChangeKind: "heartbeat",
	}
	out := formatContextReminder(u)
	assert.Contains(t, out, `origin="overlay"`, "reminder must carry origin attribute")
}

// TestFormatContextReminderOCRAndDeltaFields verifies OCRText and ChangeKind
// are included in the output when set.
func TestFormatContextReminderOCRAndDeltaFields(t *testing.T) {
	u := ContextUpdate{
		ActiveApp:  "Notes",
		Activity:   "writing",
		OCRText:    "buy milk",
		ChangeKind: "user-invoked",
	}
	out := formatContextReminder(u)
	assert.Contains(t, out, "buy milk", "OCR text must appear in reminder")
	assert.Contains(t, out, "user-invoked", "ChangeKind must appear as Delta line")
}

// --- Phase 9: synthetic_user + tracker + ack tests ---

func TestBridgeSystemReminderModeStoresReminderAndDoesNotSend(t *testing.T) {
	eng := newFakeEngine()
	bridge := NewBridgeWithMode(eng, ember.New(), nil, nil, nil, "system_reminder")

	err := bridge.OnContextUpdate(nil, ContextUpdate{
		ActiveApp:  "VS Code",
		Activity:   "coding",
		AXSummary:  "editor",
		ChangeKind: "pattern",
	})
	require.NoError(t, err)

	// Default mode: engine.Send must NOT be called.
	assert.Equal(t, 0, eng.sendCallCount(), "system_reminder mode must not call engine.Send")
	// Reminder is stored for the next turn.
	assert.NotEmpty(t, bridge.PendingSystemReminder())
	// Tracker recorded one entry.
	assert.Equal(t, 1, len(bridge.Tracker().Recent(10)))
	assert.Greater(t, bridge.Tracker().Total(), 0)
}

func TestBridgeSyntheticUserModeCallsEngineSend(t *testing.T) {
	eng := newFakeEngine()
	bridge := NewBridgeWithMode(eng, ember.New(), nil, nil, nil, "synthetic_user")

	err := bridge.OnContextUpdate(nil, ContextUpdate{
		ActiveApp:  "VS Code",
		Activity:   "coding",
		AXSummary:  "editor",
		ChangeKind: "pattern",
	})
	require.NoError(t, err)

	// engine.Send runs in a goroutine, wait briefly.
	assert.Eventually(t, func() bool {
		return eng.sendCallCount() == 1
	}, 500*time.Millisecond, 10*time.Millisecond, "synthetic_user must call engine.Send")

	assert.Contains(t, eng.sendCallAt(0), "<system-reminder origin=\"overlay\">")
	// Pending reminder should stay empty - we sent it directly.
	assert.Empty(t, bridge.PendingSystemReminder())
}

func TestBridgeSyntheticUserRateLimit10s(t *testing.T) {
	eng := newFakeEngine()
	bridge := NewBridgeWithMode(eng, ember.New(), nil, nil, nil, "synthetic_user")

	// First update: sent.
	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		ActiveApp: "VS Code", Activity: "coding", ChangeKind: "pattern",
	}))
	// Second update within 10s: rate-limited, stored as reminder.
	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		ActiveApp: "Terminal", Activity: "coding", ChangeKind: "error",
	}))

	// Give the first goroutine time to run.
	assert.Eventually(t, func() bool {
		return eng.sendCallCount() == 1
	}, 500*time.Millisecond, 10*time.Millisecond)

	// Only one send, not two.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, eng.sendCallCount(), "rate-limited: second synthetic send must not fire")

	// Second update lands in the pending reminder as fallback.
	assert.NotEmpty(t, bridge.PendingSystemReminder())

	// Both updates counted by tracker.
	assert.Equal(t, 2, len(bridge.Tracker().Recent(10)))
}

func TestBridgeContextAckBroadcastOnUpdate(t *testing.T) {
	eng := newFakeEngine()
	spy := &spyHandler{}
	srv, cancel := startTestServer(t, spy)
	defer cancel()
	defer srv.Close()

	bridge := NewBridgeWithMode(eng, ember.New(), srv, nil, nil, "system_reminder")

	conn := dialTestClient(t, srv)
	sendEnvelope(t, conn, TypeHello, Hello{PID: 1})
	readEnvelope(t, conn) // welcome

	err := bridge.OnContextUpdate(nil, ContextUpdate{
		ActiveApp:  "VS Code",
		Activity:   "coding",
		AXSummary:  "editor",
		ChangeKind: "pattern",
	})
	require.NoError(t, err)

	env := readEnvelope(t, conn)
	assert.Equal(t, TypeContextAck, env.Type)

	var ack ContextAck
	require.NoError(t, json.Unmarshal(env.Data, &ack))
	assert.Greater(t, ack.Tokens, 0)
	assert.Equal(t, "pattern", ack.Reason)
	assert.Equal(t, "system_reminder", ack.Mode)
	assert.Equal(t, ack.Tokens, ack.Total, "first update: total equals single emit")
}

func TestBridgeDefaultModeFallsBackToSystemReminder(t *testing.T) {
	// Empty mode -> default.
	bridge := NewBridgeWithMode(newFakeEngine(), ember.New(), nil, nil, nil, "")
	assert.Equal(t, "system_reminder", bridge.InjectionMode())
}

// TestBridgeRuntimePrefsAdvertisedInWelcome verifies that SetRuntimePrefs
// values are carried through OnHello into the Welcome envelope.
// Phase 10.
func TestBridgeRuntimePrefsAdvertisedInWelcome(t *testing.T) {
	bridge := NewBridge(newFakeEngine(), ember.New(), nil, nil, nil)
	bridge.SetRuntimePrefs(true, "bottom-bar", []string{"com.1password.1password", "com.apple.keychainaccess"}, "", 0, 0, "")

	w := bridge.OnHello(nil, Hello{ClientVersion: "1.0", PID: 100})
	assert.True(t, w.TTSEnabled)
	assert.Equal(t, "bottom-bar", w.Position)
	assert.Equal(t, []string{"com.1password.1password", "com.apple.keychainaccess"}, w.ExcludedApps)
}

// TestBridgeWelcomeChatDefaults verifies Welcome carries default ui_mode="ghost"
// and chat_history_limit=50 when SetRuntimePrefs has not been called.
// Phase A (chat overlay).
func TestBridgeWelcomeChatDefaults(t *testing.T) {
	bridge := NewBridge(newFakeEngine(), ember.New(), nil, nil, nil)
	w := bridge.OnHello(nil, Hello{PID: 1})
	assert.Equal(t, "ghost", w.UIMode)
	assert.Equal(t, 50, w.ChatHistoryLimit)
}

// TestBridgeWelcomeChatConfigured verifies Welcome reflects chat-mode config
// after SetRuntimePrefs passes non-default values.
// Phase A (chat overlay).
func TestBridgeWelcomeChatConfigured(t *testing.T) {
	bridge := NewBridge(newFakeEngine(), ember.New(), nil, nil, nil)
	bridge.SetRuntimePrefs(false, "right-sidebar", nil, "chat", 100, 0.9, "right")

	w := bridge.OnHello(nil, Hello{PID: 2})
	assert.Equal(t, "chat", w.UIMode)
	assert.Equal(t, 100, w.ChatHistoryLimit)
}

// TestBridgeRuntimePrefsDefaultEmpty verifies a bridge with no SetRuntimePrefs
// call returns zero values in Welcome.
func TestBridgeRuntimePrefsDefaultEmpty(t *testing.T) {
	bridge := NewBridge(newFakeEngine(), ember.New(), nil, nil, nil)
	w := bridge.OnHello(nil, Hello{PID: 1})
	assert.False(t, w.TTSEnabled)
	assert.Empty(t, w.Position)
	assert.Empty(t, w.ExcludedApps)
}

// TestBridgeRuntimePrefsSnapshotIsCopy verifies callers cannot mutate the
// bridge's internal slice via the arg they passed in.
func TestBridgeRuntimePrefsSnapshotIsCopy(t *testing.T) {
	bridge := NewBridge(newFakeEngine(), ember.New(), nil, nil, nil)
	apps := []string{"com.example.a"}
	bridge.SetRuntimePrefs(false, "right-sidebar", apps, "", 0, 0, "")
	apps[0] = "com.example.mutated"

	_, _, got := bridge.RuntimePrefs()
	assert.Equal(t, []string{"com.example.a"}, got, "bridge must hold its own copy")
}

// TestBridgeSetEngineWiresWelcome verifies that SetEngine wires a new engine
// and that OnHello includes Model and EngineType from the updated engine.
func TestBridgeSetEngineWiresWelcome(t *testing.T) {
	// Start with no engine.
	bridge := NewBridge(nil, ember.New(), nil, nil, nil)

	eng := newFakeEngine()
	eng.model = "opus"
	eng.engineType = "claude"
	bridge.SetEngine(eng)

	w := bridge.OnHello(nil, Hello{ClientVersion: "1.0", PID: 42})
	assert.Equal(t, "opus", w.Model)
	assert.Equal(t, "claude", w.Engine)
}

// TestBridgeSetEngineNilSafe verifies that a nil engine produces empty
// engine/model fields in Welcome without panicking.
func TestBridgeSetEngineNilSafe(t *testing.T) {
	bridge := NewBridge(nil, ember.New(), nil, nil, nil)
	bridge.SetEngine(nil)

	w := bridge.OnHello(nil, Hello{ClientVersion: "1.0", PID: 1})
	assert.Empty(t, w.Engine)
	assert.Empty(t, w.Model)
}

// TestContextUpdateOriginField verifies the Origin field is present in
// ContextUpdate and round-trips through JSON.
func TestContextUpdateOriginField(t *testing.T) {
	u := ContextUpdate{
		ActiveApp: "Terminal",
		Origin:    "overlay",
	}
	b, err := json.Marshal(u)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"origin":"overlay"`)

	var u2 ContextUpdate
	require.NoError(t, json.Unmarshal(b, &u2))
	assert.Equal(t, "overlay", u2.Origin)
}

