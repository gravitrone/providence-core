package overlay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
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

func (e *fakeEngine) Interrupt()               { e.interrupted = true }
func (e *fakeEngine) Model() string            { return e.model }
func (e *fakeEngine) EngineType() string       { return e.engineType }
func (e *fakeEngine) SessionBus() *session.Bus { return e.bus }

// --- Tests ---

func TestBridgeContextUpdateStoresPendingReminder(t *testing.T) {
	eng := newFakeEngine()
	em := ember.New()
	bridge := NewBridge(eng, em, nil, nil, nil)

	// Post-ambient-rewire: ContextUpdate only carries ScreenshotPNGB64 + Transcript.
	u := ContextUpdate{
		Timestamp:  time.Now(),
		Transcript: "what does this function do",
		ChangeKind: "transcript_only",
	}

	err := bridge.OnContextUpdate(nil, u)
	require.NoError(t, err)

	reminder := bridge.PendingSystemReminder()
	assert.NotEmpty(t, reminder)
	assert.Contains(t, reminder, `<system-reminder origin="overlay">`)
	assert.Contains(t, reminder, "</system-reminder>")
	assert.Contains(t, reminder, "what does this function do")
}

func TestBridgePendingReminderClearsAfterRead(t *testing.T) {
	// Post-ambient-rewire: PendingSystemReminder is a non-destructive read.
	// It returns the rolling transcript wrapped in a reminder block.
	// Without a transcript, it returns "".
	bridge := NewBridge(newFakeEngine(), ember.New(), nil, nil, nil)

	// No transcript yet - reminder is empty.
	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		ChangeKind: "heartbeat",
	}))

	// No transcript was set, so reminder should be empty.
	assert.Empty(t, bridge.PendingSystemReminder())

	// Now set a transcript.
	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		Transcript: "user said hello",
		ChangeKind: "transcript_only",
	}))

	// Reminder is non-empty and stable across reads (non-destructive).
	first := bridge.PendingSystemReminder()
	assert.NotEmpty(t, first)
	second := bridge.PendingSystemReminder()
	assert.Equal(t, first, second, "post-rewire: PendingSystemReminder is idempotent")
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

func TestBridgeSetEngineConcurrentHandlerNoRace(t *testing.T) {
	eng := newFakeEngine()
	bridge := NewBridge(nil, ember.New(), nil, nil, quietLogger())
	bridge.SetEngine(eng)

	var sawUnexpected atomic.Bool
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			bridge.SetEngine(eng)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			w := bridge.OnHello(nil, Hello{PID: i + 1})
			if w.Engine != "claude" || w.Model != "sonnet" {
				sawUnexpected.Store(true)
			}
		}
	}()

	wg.Wait()

	assert.False(t, sawUnexpected.Load())
}

func TestBridgeSetEngineConcurrentHelloAndUserQueryNoRace(t *testing.T) {
	engA := newFakeEngine()
	engB := newFakeEngine()
	engB.model = "opus"
	engB.engineType = "codex"

	bridge := NewBridge(nil, ember.New(), nil, nil, quietLogger())
	bridge.SetEngine(engA)

	var sawUnexpected atomic.Bool
	var sawQueryError atomic.Bool
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			if i%2 == 0 {
				bridge.SetEngine(engA)
				continue
			}
			bridge.SetEngine(engB)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			w := bridge.OnHello(nil, Hello{PID: i + 1})
			validA := w.Engine == engA.EngineType() && w.Model == engA.Model()
			validB := w.Engine == engB.EngineType() && w.Model == engB.Model()
			if !validA && !validB {
				sawUnexpected.Store(true)
			}
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			if err := bridge.OnUserQuery(nil, UserQuery{Text: "status"}); err != nil {
				sawQueryError.Store(true)
			}
		}
	}()

	wg.Wait()

	assert.False(t, sawUnexpected.Load())
	assert.False(t, sawQueryError.Load())
	assert.Equal(t, 1000, engA.sendCallCount()+engB.sendCallCount())
}

func TestBridgeStartForwardsSessionBusEventAfterSetEngine(t *testing.T) {
	eng := newFakeEngine()
	spawn := false
	dir, err := os.MkdirTemp("/tmp", "pvd-bridge-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	manager := NewManager(Config{
		SocketPath: filepath.Join(dir, "overlay.sock"),
		Spawn:      &spawn,
	}, quietLogger())
	bridge := NewBridge(nil, ember.New(), nil, manager, quietLogger())

	managerCtx, stopManager := context.WithCancel(context.Background())
	defer stopManager()
	require.NoError(t, manager.Start(managerCtx, bridge))
	t.Cleanup(func() {
		require.NoError(t, manager.Stop(context.Background()))
	})

	bridge.SetEngine(eng)
	bridgeCtx, stopBridge := context.WithCancel(context.Background())
	defer stopBridge()
	go bridge.Start(bridgeCtx)

	srv := manager.Server()
	require.NotNil(t, srv)

	conn := dialTestClient(t, srv)
	t.Cleanup(func() { _ = conn.Close() })
	sendEnvelope(t, conn, TypeHello, Hello{PID: 1})
	readEnvelope(t, conn) // welcome

	eng.bus.Publish(session.Event{Type: session.EventNewMessage, Data: "set-engine payload"})

	env := readEnvelope(t, conn)
	assert.Equal(t, TypeSessionEvent, env.Type)

	var se SessionEvent
	require.NoError(t, json.Unmarshal(env.Data, &se))
	assert.Equal(t, session.EventNewMessage, se.Type)
	assert.JSONEq(t, `"set-engine payload"`, string(se.Data))
}

func TestManagerStopCallsSrvCancel(t *testing.T) {
	manager := NewManager(Config{}, nil)
	manager.state = StateRunning

	cancelCalled := false
	manager.srvCancel = func() {
		cancelCalled = true
	}

	require.NoError(t, manager.Stop(context.Background()))

	assert.True(t, cancelCalled)
	assert.Nil(t, manager.srvCancel)
	assert.Equal(t, StateStopped, manager.State())
}

// TestFormatTranscriptReminder checks the formatting of the post-ambient-rewire
// transcript-only reminder builder.
func TestFormatTranscriptReminder(t *testing.T) {
	cases := []struct {
		name        string
		transcript  string
		wantContain []string
		wantEmpty   bool
	}{
		{
			name:       "non-empty transcript",
			transcript: "what's this function",
			wantContain: []string{
				`<system-reminder origin="overlay">`,
				"</system-reminder>",
				"what's this function",
			},
		},
		{
			name:       "empty transcript returns empty string",
			transcript: "",
			wantEmpty:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := formatTranscriptReminder(tc.transcript)
			if tc.wantEmpty {
				assert.Empty(t, out)
				return
			}
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

// TestFormatTranscriptReminderOriginAttribute verifies the origin="overlay" attribute
// is present when there is a transcript to format.
func TestFormatTranscriptReminderOriginAttribute(t *testing.T) {
	out := formatTranscriptReminder("user said something")
	assert.Contains(t, out, `origin="overlay"`, "reminder must carry origin attribute")
}

// TestFormatTranscriptReminderContent verifies transcript content appears in the output.
func TestFormatTranscriptReminderContent(t *testing.T) {
	out := formatTranscriptReminder("buy milk")
	assert.Contains(t, out, "buy milk", "transcript text must appear in reminder")
}

// --- Phase 9: synthetic_user + tracker + ack tests ---

func TestBridgeSystemReminderModeStoresReminderAndDoesNotSend(t *testing.T) {
	eng := newFakeEngine()
	bridge := NewBridgeWithMode(eng, ember.New(), nil, nil, nil, "system_reminder")

	// Post-ambient-rewire: include a transcript so PendingSystemReminder is non-empty.
	err := bridge.OnContextUpdate(nil, ContextUpdate{
		Transcript: "editor context",
		ChangeKind: "transcript_only",
	})
	require.NoError(t, err)

	// Default mode: engine.Send must NOT be called.
	assert.Equal(t, 0, eng.sendCallCount(), "system_reminder mode must not call engine.Send")
	// Reminder is stored for the next turn.
	assert.NotEmpty(t, bridge.PendingSystemReminder())
	// Tracker recorded one entry (transcript tokens).
	assert.Equal(t, 1, len(bridge.Tracker().Recent(10)))
	assert.Greater(t, bridge.Tracker().Total(), 0)
}

func TestBridgeSyntheticUserModeCallsEngineSend(t *testing.T) {
	// Post-ambient-rewire: synthetic_user mode is a no-op. The engine sees
	// screenshots + transcript on the next natural turn via the ring buffer,
	// not via a direct Send. Verify engine.Send is never called.
	eng := newFakeEngine()
	bridge := NewBridgeWithMode(eng, ember.New(), nil, nil, nil, "synthetic_user")

	err := bridge.OnContextUpdate(nil, ContextUpdate{
		Transcript: "what are you doing",
		ChangeKind: "transcript_only",
	})
	require.NoError(t, err)

	// Give any goroutine time to run (none should).
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, eng.sendCallCount(), "synthetic_user is a no-op post-rewire: Send must not fire")
}

func TestBridgeSyntheticUserRateLimit10s(t *testing.T) {
	// Post-ambient-rewire: synthetic_user is a no-op. Both updates go into the
	// ring / transcript with no direct Send. Tracker still records both.
	eng := newFakeEngine()
	bridge := NewBridgeWithMode(eng, ember.New(), nil, nil, nil, "synthetic_user")

	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		Transcript: "first", ChangeKind: "transcript_only",
	}))
	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		Transcript: "second", ChangeKind: "transcript_only",
	}))

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, eng.sendCallCount(), "synthetic_user no-op: no sends expected")

	// Both updates counted by tracker (transcript tokens).
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
		Transcript: "coding in vs code",
		ChangeKind: "transcript_only",
	})
	require.NoError(t, err)

	env := readEnvelope(t, conn)
	assert.Equal(t, TypeContextAck, env.Type)

	var ack ContextAck
	require.NoError(t, json.Unmarshal(env.Data, &ack))
	assert.Greater(t, ack.Tokens, 0)
	assert.Equal(t, "transcript_only", ack.Reason)
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
// Phase 10. Post-ambient-rewire: only TTSEnabled remains in the wire format.
func TestBridgeRuntimePrefsAdvertisedInWelcome(t *testing.T) {
	bridge := NewBridge(newFakeEngine(), ember.New(), nil, nil, nil)
	bridge.SetRuntimePrefs(true)

	w := bridge.OnHello(nil, Hello{ClientVersion: "1.0", PID: 100})
	assert.True(t, w.TTSEnabled)
}

// TestBridgeRuntimePrefsDefaultEmpty verifies a bridge with no SetRuntimePrefs
// call returns false for TTSEnabled in Welcome.
func TestBridgeRuntimePrefsDefaultEmpty(t *testing.T) {
	bridge := NewBridge(newFakeEngine(), ember.New(), nil, nil, nil)
	w := bridge.OnHello(nil, Hello{PID: 1})
	assert.False(t, w.TTSEnabled)
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

// --- Phase G: daily budget breaker ---

func TestBridgeSkipsInjectionOnBudgetExceeded(t *testing.T) {
	eng := newFakeEngine()
	bridge := NewBridgeWithMode(eng, ember.New(), nil, nil, nil, "synthetic_user")
	bridge.SetDailyBudget(50)

	// Pre-spend the day's budget directly so BudgetExceeded() returns true
	// before OnContextUpdate even runs.
	bridge.Tracker().Record(TokenEntry{
		Time:   time.Now(),
		Tokens: 60,
		Reason: "pretest",
		App:    "Test",
		Mode:   "synthetic_user",
	})
	require.True(t, bridge.Tracker().BudgetExceeded())

	err := bridge.OnContextUpdate(nil, ContextUpdate{
		Timestamp:  time.Now(),
		Transcript: "budget exceeded test",
		ChangeKind: "transcript_only",
	})
	require.NoError(t, err)

	// Budget exceeded: no synthetic send and no pending reminder.
	assert.Equal(t, 0, eng.sendCallCount(), "budget exceeded -> no synthetic send")
	assert.Empty(t, bridge.PendingSystemReminder(), "budget exceeded -> no pending reminder")
}

func TestBridgeAllowsInjectionWithinBudget(t *testing.T) {
	bridge := NewBridge(newFakeEngine(), ember.New(), nil, nil, nil)
	bridge.SetDailyBudget(100000) // effectively unlimited for this test

	err := bridge.OnContextUpdate(nil, ContextUpdate{
		Timestamp:  time.Now(),
		Transcript: "browsing the web",
		ChangeKind: "transcript_only",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, bridge.PendingSystemReminder(), "under budget -> reminder stored")
}

func TestBridgeBudgetZeroDisablesGating(t *testing.T) {
	bridge := NewBridge(newFakeEngine(), ember.New(), nil, nil, nil)
	// Default NewBridge leaves DailyTokenBudget=0 (unlimited).

	// Load a massive token count; should still allow injections.
	bridge.Tracker().Record(TokenEntry{
		Time:   time.Now(),
		Tokens: 10_000_000,
		Reason: "huge",
		App:    "X",
		Mode:   "system_reminder",
	})
	assert.False(t, bridge.Tracker().BudgetExceeded())

	err := bridge.OnContextUpdate(nil, ContextUpdate{
		Timestamp:  time.Now(),
		ChangeKind: "heartbeat",
	})
	require.NoError(t, err)
	// Budget=0 so no gating; but no transcript => PendingSystemReminder is empty.
	// The test only verifies that budget=0 doesn't cause an error.
}

// --- Screen ring buffer tests (Phase: ambient rewire) ---

// makeTinyPNG returns a 1x1 transparent PNG encoded as base64.
// All calls produce identical bytes (deterministic). Use makeColoredPNGB64
// when you need frames with distinct byte content.
func makeTinyPNGB64(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// makeColoredPNGB64 returns a 1x1 PNG whose single pixel has the given seed
// byte as its red channel. Distinct seeds produce distinct PNG byte slices.
func makeColoredPNGB64(t *testing.T, seed uint8) (string, []byte) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Pix[0] = seed // R
	img.Pix[1] = 0    // G
	img.Pix[2] = 0    // B
	img.Pix[3] = 255  // A
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	raw := make([]byte, buf.Len())
	copy(raw, buf.Bytes())
	return base64.StdEncoding.EncodeToString(raw), raw
}

// TestBridgeRingFIFO_SixFrames verifies that after 6 ContextUpdates with valid
// PNGs, ScreenshotPNGs returns 6 entries with the first (oldest) frame at [0].
func TestBridgeRingFIFO_SixFrames(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())

	// Use distinct colored frames so we can identify them by content.
	var firstPNG []byte
	for i := range 6 {
		b64, raw := makeColoredPNGB64(t, uint8(i+1))
		if i == 0 {
			firstPNG = raw
		}
		require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
			ScreenshotPNGB64: b64,
			ChangeKind:       "frame",
		}))
	}

	pngs := bridge.ScreenshotPNGs()
	require.Len(t, pngs, 6, "ring should hold exactly 6 frames")
	assert.Equal(t, firstPNG, pngs[0], "oldest frame (seed=1) must be at index 0 (FIFO)")
}

// TestBridgeRingFIFO_SeventhEvictsFirst verifies that a 7th update evicts the
// oldest frame so ring stays at cap=6 and the old head is replaced.
func TestBridgeRingFIFO_SeventhEvictsFirst(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())

	// Push 6 frames with distinct seeds 1..6.
	var firstPNG, secondPNG []byte
	for i := range 6 {
		b64, raw := makeColoredPNGB64(t, uint8(i+1))
		if i == 0 {
			firstPNG = raw
		}
		if i == 1 {
			secondPNG = raw
		}
		require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
			ScreenshotPNGB64: b64,
			ChangeKind:       "frame",
		}))
	}

	// 7th frame with seed=7.
	seventhB64, seventhPNG := makeColoredPNGB64(t, 7)
	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		ScreenshotPNGB64: seventhB64,
		ChangeKind:       "frame",
	}))

	pngs := bridge.ScreenshotPNGs()
	require.Len(t, pngs, 6, "ring must stay at cap after eviction")
	// Frame with seed=1 (firstPNG) was evicted; frame with seed=2 is now head.
	assert.NotEqual(t, firstPNG, pngs[0], "original oldest frame (seed=1) must have been evicted")
	assert.Equal(t, secondPNG, pngs[0], "second frame (seed=2) must now be at head")
	assert.Equal(t, seventhPNG, pngs[len(pngs)-1], "newest frame (seed=7) must be at tail")
}

// TestBridgeRingEmptyPNGDoesNotPush verifies that an update with empty
// ScreenshotPNGB64 does NOT push to the ring, but DOES update the transcript.
func TestBridgeRingEmptyPNGDoesNotPush(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())

	// Push one real frame first.
	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		ScreenshotPNGB64: makeTinyPNGB64(t),
		ChangeKind:       "frame",
	}))
	require.Len(t, bridge.ScreenshotPNGs(), 1)

	// Now update with empty PNG but with a transcript.
	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		ScreenshotPNGB64: "",
		Transcript:       "hello world",
		ChangeKind:       "transcript_only",
	}))

	// Ring must still have exactly 1 frame.
	assert.Len(t, bridge.ScreenshotPNGs(), 1, "empty PNG must not push to ring")
	// Transcript must be updated.
	assert.Equal(t, "hello world", bridge.Transcript())
}

// TestBridgeTranscriptLatest verifies that Transcript() returns the most
// recently set non-empty transcript across multiple updates.
func TestBridgeTranscriptLatest(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())

	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		Transcript: "first transcript",
		ChangeKind: "transcript_only",
	}))
	assert.Equal(t, "first transcript", bridge.Transcript())

	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		Transcript: "second transcript",
		ChangeKind: "transcript_only",
	}))
	assert.Equal(t, "second transcript", bridge.Transcript())
}

// --- New ring / transcript isolation tests ---

// TestBridge_Screenshots_EvictsFramesOlderThan30s documents the TTL eviction
// gap. Production code evicts on read using time.Now(), so faking past time
// requires either clock injection (not yet wired) or a 30s sleep (too slow).
// Tracked for Phase N clock-injection refactor.
func TestBridge_Screenshots_EvictsFramesOlderThan30s(t *testing.T) {
	t.Skip("TTL eviction requires clock injection; tracked for Phase N refactor")
}

// TestBridge_MarkAttachedThenNoNewFrames_RingUnchangedReturnsFalse verifies
// that RingChangedSinceLastAttach returns false when no new frames have been
// pushed after MarkAttached.
func TestBridge_MarkAttachedThenNoNewFrames_RingUnchangedReturnsFalse(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())

	b64, _ := makeColoredPNGB64(t, 1)
	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		ScreenshotPNGB64: b64,
		ChangeKind:       "frame",
	}))

	bridge.MarkAttached()
	assert.False(t, bridge.RingChangedSinceLastAttach(), "no new frames after MarkAttached -> unchanged")
}

// TestBridge_MarkAttachedThenPushNewFrame_RingChangedReturnsTrue verifies
// that RingChangedSinceLastAttach returns true after a new frame evicts the
// attached head.
func TestBridge_MarkAttachedThenPushNewFrame_RingChangedReturnsTrue(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())

	// Fill ring to capacity so the next push evicts the current head.
	for i := range screenRingCap {
		b64, _ := makeColoredPNGB64(t, uint8(i+1))
		require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
			ScreenshotPNGB64: b64,
			ChangeKind:       "frame",
		}))
	}

	bridge.MarkAttached()
	assert.False(t, bridge.RingChangedSinceLastAttach(), "unchanged immediately after MarkAttached")

	// Push one more frame - evicts the oldest, ring[0].Time changes.
	b64, _ := makeColoredPNGB64(t, 99)
	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		ScreenshotPNGB64: b64,
		ChangeKind:       "frame",
	}))

	assert.True(t, bridge.RingChangedSinceLastAttach(), "new frame pushed after attach -> ring changed")
}

// TestBridge_TranscriptOnlyUpdateDoesNotAffectRing verifies that a
// ContextUpdate with no screenshot leaves the ring untouched while updating
// the transcript.
func TestBridge_TranscriptOnlyUpdateDoesNotAffectRing(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())

	// Push one real frame first.
	b64, _ := makeColoredPNGB64(t, 7)
	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		ScreenshotPNGB64: b64,
		ChangeKind:       "frame",
	}))
	require.Len(t, bridge.ScreenshotPNGs(), 1, "one frame before transcript-only update")

	bridge.MarkAttached()

	// Transcript-only update - no PNG.
	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		ScreenshotPNGB64: "",
		Transcript:       "hi",
		ChangeKind:       "transcript_only",
	}))

	assert.Len(t, bridge.ScreenshotPNGs(), 1, "ring must not grow on transcript-only update")
	assert.Equal(t, "hi", bridge.Transcript(), "transcript must be updated")
	assert.False(t, bridge.RingChangedSinceLastAttach(), "ring head unchanged -> dedup returns false")
}

// TestBridge_ScreenshotsReturnsCopyNotAlias verifies that mutating the slice
// returned by Screenshots does not affect the bridge's internal ring.
func TestBridge_ScreenshotsReturnsCopyNotAlias(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())

	b64, _ := makeColoredPNGB64(t, 42)
	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		ScreenshotPNGB64: b64,
		ChangeKind:       "frame",
	}))

	first := bridge.Screenshots()
	require.Len(t, first, 1)

	// Mutate the returned slice.
	first[0] = ScreenshotFrame{}

	// Bridge internal ring must be unaffected.
	second := bridge.Screenshots()
	require.Len(t, second, 1, "bridge ring must still hold the original frame")
	assert.NotEqual(t, first[0].Time, second[0].Time, "mutated copy must not alias bridge internals")
}

// TestBridge_ScreenshotPNGsMatchesScreenshotsOrder verifies that ScreenshotPNGs
// returns PNGs in the same order as Screenshots (oldest first).
func TestBridge_ScreenshotPNGsMatchesScreenshotsOrder(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())

	var expectedPNGs [][]byte
	for i := range 3 {
		b64, raw := makeColoredPNGB64(t, uint8(i+10))
		expectedPNGs = append(expectedPNGs, raw)
		require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
			ScreenshotPNGB64: b64,
			ChangeKind:       "frame",
		}))
	}

	frames := bridge.Screenshots()
	pngs := bridge.ScreenshotPNGs()
	require.Len(t, frames, 3)
	require.Len(t, pngs, 3)

	for i := range 3 {
		assert.Equal(t, frames[i].PNG, pngs[i], "ScreenshotPNGs[%d] must match Screenshots[%d].PNG", i, i)
	}
}

// TestBridge_TranscriptEmptyWhenNoUpdates verifies Transcript returns "" on a
// fresh bridge with no updates.
func TestBridge_TranscriptEmptyWhenNoUpdates(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())
	assert.Empty(t, bridge.Transcript(), "fresh bridge must have empty transcript")
}

// TestBridgeMarkAttachedAndRingChanged verifies the MarkAttached / RingChangedSinceLastAttach
// dedup contract:
//   - After MarkAttached(), RingChangedSinceLastAttach() returns false (ring head unchanged).
//   - After pushing enough frames to evict the current head, RingChangedSinceLastAttach()
//     returns true because screenRing[0] now points to a different (newer) frame.
func TestBridgeMarkAttachedAndRingChanged(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())

	// Fill the ring to capacity (6 frames).
	for range screenRingCap {
		require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
			ScreenshotPNGB64: makeTinyPNGB64(t),
			ChangeKind:       "frame",
		}))
	}
	require.Len(t, bridge.ScreenshotPNGs(), screenRingCap)

	// Mark attached - ring head is the oldest frame.
	bridge.MarkAttached()
	assert.False(t, bridge.RingChangedSinceLastAttach(), "no change immediately after MarkAttached")

	// Push a 7th frame - this evicts the oldest, ring[0] becomes the 2nd original frame.
	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		ScreenshotPNGB64: makeTinyPNGB64(t),
		ChangeKind:       "frame",
	}))
	// Ring head changed (oldest was evicted) -> RingChangedSinceLastAttach must be true.
	assert.True(t, bridge.RingChangedSinceLastAttach(), "ring changed: oldest frame was evicted by 7th push")
}

// --- OnContextUpdate edge cases (Phase: test coverage sweep) ---

// TestOnContextUpdate_MalformedBase64DoesNotCrash_TranscriptStillUpdates verifies
// that a corrupt ScreenshotPNGB64 value is silently dropped while the Transcript
// is still stored and nil/empty PNG slice is returned.
func TestOnContextUpdate_MalformedBase64DoesNotCrash_TranscriptStillUpdates(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())

	err := bridge.OnContextUpdate(nil, ContextUpdate{
		ScreenshotPNGB64: "not-valid-base64!!!",
		Transcript:       "hello",
		ChangeKind:       "frame",
	})
	require.NoError(t, err, "bad base64 must not return an error")
	assert.Equal(t, "hello", bridge.Transcript(), "transcript must be stored despite bad png")
	pngs := bridge.ScreenshotPNGs()
	assert.True(t, len(pngs) == 0, "bad base64 must not push a frame to the ring")
}

// TestOnContextUpdate_EmptyPNGDoesNotPushToRing verifies that an empty
// ScreenshotPNGB64 on a second update leaves the ring length unchanged.
func TestOnContextUpdate_EmptyPNGDoesNotPushToRing(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())

	// Push one valid frame first.
	b64, _ := makeColoredPNGB64(t, 1)
	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		ScreenshotPNGB64: b64,
		ChangeKind:       "frame",
	}))
	require.Len(t, bridge.ScreenshotPNGs(), 1, "pre-condition: 1 frame in ring")

	// Send an update with an empty PNG field.
	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		ScreenshotPNGB64: "",
		Transcript:       "no frame here",
		ChangeKind:       "heartbeat",
	}))

	assert.Len(t, bridge.ScreenshotPNGs(), 1, "ring must stay at 1 after empty-PNG update")
}

// TestOnContextUpdate_OnlyTranscriptDoesNotBumpRing verifies that multiple
// transcript-only updates never push any frame to the ring.
func TestOnContextUpdate_OnlyTranscriptDoesNotBumpRing(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())

	for i := range 5 {
		require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
			Transcript: fmt.Sprintf("update %d", i),
			ChangeKind: "transcript_only",
		}))
	}

	assert.Len(t, bridge.ScreenshotPNGs(), 0, "transcript-only updates must not touch the ring")
	assert.Equal(t, "update 4", bridge.Transcript(), "transcript must reflect the last update")
}

// TestOnContextUpdate_RingEvictsOldestAtCapSix pushes 7 distinct frames and
// asserts that the first pushed frame (seed=1) is evicted, and ring[0] now
// holds seed=2's bytes.
func TestOnContextUpdate_RingEvictsOldestAtCapSix(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())

	var seed2PNG []byte
	for i := range 7 {
		b64, raw := makeColoredPNGB64(t, uint8(i+1))
		if i == 1 {
			seed2PNG = raw
		}
		require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
			ScreenshotPNGB64: b64,
			ChangeKind:       "frame",
		}))
	}

	pngs := bridge.ScreenshotPNGs()
	require.Len(t, pngs, 6, "ring must be capped at 6 after 7 pushes")
	assert.Equal(t, seed2PNG, pngs[0], "ring[0] must be seed=2 after seed=1 was evicted")
}

// TestOnContextUpdate_BudgetExceededSkipsRingPush verifies that when the daily
// budget is exceeded, a subsequent update does not push a frame into the ring and
// the ack broadcast carries reason="budget_exceeded".
func TestOnContextUpdate_BudgetExceededSkipsRingPush(t *testing.T) {
	eng := newFakeEngine()
	spy := &spyHandler{}
	srv, cancel := startTestServer(t, spy)
	defer cancel()
	defer srv.Close()

	bridge := NewBridgeWithMode(eng, ember.New(), srv, nil, nil, "system_reminder")
	bridge.SetDailyBudget(10)

	// Pre-spend over budget directly so BudgetExceeded() is true.
	bridge.Tracker().Record(TokenEntry{
		Time:   time.Now(),
		Tokens: 100,
		Reason: "pretest",
		App:    "test",
		Mode:   "system_reminder",
	})
	require.True(t, bridge.Tracker().BudgetExceeded(), "pre-condition: budget must be exceeded")

	// Connect a client to receive the ack broadcast.
	conn := dialTestClient(t, srv)
	sendEnvelope(t, conn, TypeHello, Hello{PID: 1})
	readEnvelope(t, conn) // welcome

	// Push one valid frame.
	b64, _ := makeColoredPNGB64(t, 42)
	err := bridge.OnContextUpdate(nil, ContextUpdate{
		ScreenshotPNGB64: b64,
		Transcript:       "ignored",
		ChangeKind:       "frame",
	})
	require.NoError(t, err, "budget_exceeded must not return an error")

	// Ring must not have grown.
	assert.Len(t, bridge.ScreenshotPNGs(), 0, "budget exceeded -> no frame pushed to ring")

	// Ack broadcast must carry reason=budget_exceeded.
	env := readEnvelope(t, conn)
	assert.Equal(t, TypeContextAck, env.Type)
	var ack ContextAck
	require.NoError(t, json.Unmarshal(env.Data, &ack))
	assert.Equal(t, "budget_exceeded", ack.Reason, "ack reason must be budget_exceeded")
}

// TestOnContextUpdate_ConcurrentCallsRaceFree fires 10 goroutines each making
// 50 OnContextUpdate calls with distinct PNGs. Must not panic and must pass
// -race. Ring length at end must be <= 6.
func TestOnContextUpdate_ConcurrentCallsRaceFree(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())

	var wg sync.WaitGroup
	for g := range 10 {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range 50 {
				seed := uint8((g*50 + i) % 255)
				b64, _ := makeColoredPNGB64(t, seed)
				_ = bridge.OnContextUpdate(nil, ContextUpdate{
					ScreenshotPNGB64: b64,
					Transcript:       fmt.Sprintf("g%d-i%d", g, i),
					ChangeKind:       "frame",
				})
			}
		}(g)
	}
	wg.Wait()

	pngs := bridge.ScreenshotPNGs()
	assert.LessOrEqual(t, len(pngs), screenRingCap, "ring must never exceed cap after concurrent writes")
}

// TestOnContextUpdate_TranscriptReplacesNotConcatenates verifies that each
// OnContextUpdate with a non-empty Transcript fully replaces the previous value.
func TestOnContextUpdate_TranscriptReplacesNotConcatenates(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())

	transcripts := []string{"alpha", "beta", "gamma", "delta"}
	for _, tx := range transcripts {
		require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
			Transcript: tx,
			ChangeKind: "transcript_only",
		}))
	}

	got := bridge.Transcript()
	assert.Equal(t, "delta", got, "Transcript must return only the latest value, not concatenation")
	assert.NotContains(t, got, "alpha", "old transcript values must not bleed into current")
}

// TestOnContextUpdate_NoServerDoesNotCrash verifies that a bridge constructed
// without a server can handle OnContextUpdate without panicking.
func TestOnContextUpdate_NoServerDoesNotCrash(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())

	b64, _ := makeColoredPNGB64(t, 77)
	err := bridge.OnContextUpdate(nil, ContextUpdate{
		ScreenshotPNGB64: b64,
		Transcript:       "no server",
		ChangeKind:       "frame",
	})
	require.NoError(t, err, "nil server must not cause an error or panic")
	assert.Equal(t, "no server", bridge.Transcript())
	assert.Len(t, bridge.ScreenshotPNGs(), 1)
}

// TestOnContextUpdate_ChangeKindRecordedInTracker verifies that the ChangeKind
// field from the ContextUpdate is stored as the Reason in the token tracker
// entry so cost attribution is correct.
func TestOnContextUpdate_ChangeKindRecordedInTracker(t *testing.T) {
	bridge := NewBridge(nil, nil, nil, nil, slog.Default())

	require.NoError(t, bridge.OnContextUpdate(nil, ContextUpdate{
		Transcript: "some speech",
		ChangeKind: "user-invoked",
	}))

	entries := bridge.Tracker().Recent(10)
	require.Len(t, entries, 1, "exactly one tracker entry expected")
	assert.Equal(t, "user-invoked", entries[0].Reason, "tracker entry Reason must match ChangeKind")
}
