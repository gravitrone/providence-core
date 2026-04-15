package ui

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gravitrone/providence-core/internal/config"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/ember"
	"github.com/gravitrone/providence-core/internal/engine/session"
	"github.com/gravitrone/providence-core/internal/overlay"
)

// --- Fakes ---

// emberTestEngine implements engine.Engine with just enough surface for the
// /ember activation tests. Only Send is observed; everything else is a
// no-op that satisfies the interface.
type emberTestEngine struct {
	sent             []string
	sentMu           sync.Mutex
	events           chan engine.ParsedEvent
	interruptedCount int32
}

func newEmberTestEngine() *emberTestEngine {
	return &emberTestEngine{events: make(chan engine.ParsedEvent, 4)}
}

func (e *emberTestEngine) Send(text string) error {
	e.sentMu.Lock()
	defer e.sentMu.Unlock()
	e.sent = append(e.sent, text)
	return nil
}

func (e *emberTestEngine) SentMessages() []string {
	e.sentMu.Lock()
	defer e.sentMu.Unlock()
	out := make([]string, len(e.sent))
	copy(out, e.sent)
	return out
}

func (e *emberTestEngine) Events() <-chan engine.ParsedEvent   { return e.events }
func (e *emberTestEngine) RespondPermission(_, _ string) error { return nil }
func (e *emberTestEngine) Interrupt()                          { atomic.AddInt32(&e.interruptedCount, 1) }
func (e *emberTestEngine) InterruptCount() int32               { return atomic.LoadInt32(&e.interruptedCount) }
func (e *emberTestEngine) Cancel()                             {}
func (e *emberTestEngine) Close()                                          {}
func (e *emberTestEngine) Status() engine.SessionStatus                    { return engine.StatusIdle }
func (e *emberTestEngine) RestoreHistory(_ []engine.RestoredMessage) error { return nil }
func (e *emberTestEngine) TriggerCompact(_ context.Context) error          { return nil }
func (e *emberTestEngine) SessionBus() *session.Bus                        { return session.NewBus() }

// emberTestOverlayMgr is a minimal overlayManager whose StatusInfo always
// reports "stopped" so the /ember path treats it as not-yet-running.
type emberTestOverlayMgr struct{}

func (m *emberTestOverlayMgr) Start(_ context.Context, _ overlay.ServerHandler) error { return nil }
func (m *emberTestOverlayMgr) Stop(_ context.Context) error                            { return nil }
func (m *emberTestOverlayMgr) StatusInfo() map[string]any {
	return map[string]any{"state": "stopped"}
}

// newEmberTestBridge returns a real overlay.Bridge. We use the production
// type because overlay.ServerHandler has unexported parameters that cannot
// be implemented outside the overlay package. The bridge is never called
// during /ember tests - only its nil-ness is checked.
func newEmberTestBridge(em *ember.State) overlay.Injector {
	mgr := overlay.NewManager(overlay.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return overlay.NewBridgeWithMode(nil, em, nil, mgr, slog.New(slog.NewTextHandler(io.Discard, nil)), "")
}

// newEmberTestAgentTab builds an AgentTab wired for /ember activation
// tests: ember state ready, fake engine present, overlay manager+bridge
// present, and caller-supplied Overlay config (so tests can flip Spawn).
func newEmberTestAgentTab(t *testing.T, overlayCfg config.OverlayConfig) (*AgentTab, *emberTestEngine) {
	t.Helper()

	cfg := config.Config{Overlay: overlayCfg}
	at := NewAgentTab("", cfg, nil, nil)

	eng := newEmberTestEngine()
	at.engine = eng
	at.ember = ember.New()
	at.overlayMgr = &emberTestOverlayMgr{}
	// Real overlay bridge; /ember only checks non-nil.
	bridge, _ := newEmberTestBridge(at.ember).(interface {
		overlay.ServerHandler
		overlay.Injector
	})
	at.overlayBridge = bridge

	return &at, eng
}

// emberBoolPtr returns a pointer to the given bool. Used to set
// config.OverlayConfig.Spawn which is a *bool tri-state.
func emberBoolPtr(b bool) *bool { return &b }

// --- Tests ---

// TestOverlayLauncherSeamSwappable verifies the package-level
// overlayLauncher var can be reassigned and restored via t.Cleanup. The
// ember tests below depend on this pattern.
func TestOverlayLauncherSeamSwappable(t *testing.T) {
	orig := overlayLauncher
	t.Cleanup(func() { overlayLauncher = orig })

	var called int32
	overlayLauncher = func(_, _ string) error {
		atomic.AddInt32(&called, 1)
		return nil
	}
	require.NoError(t, overlayLauncher("app", "sock"))
	assert.Equal(t, int32(1), atomic.LoadInt32(&called))
}

// TestEmberActivationInvokesOverlayLauncherWhenSpawnDisabled verifies
// that /ember activation triggers the fallback launcher when the
// overlay is wired but Spawn is explicitly false. This is the TCC
// detach workaround path: the manager runs only the UDS server, so
// /ember must open the app bundle directly.
func TestEmberActivationInvokesOverlayLauncherWhenSpawnDisabled(t *testing.T) {
	orig := overlayLauncher
	t.Cleanup(func() { overlayLauncher = orig })

	var callCount int32
	var gotApp, gotSocket string
	var mu sync.Mutex
	overlayLauncher = func(app, socket string) error {
		atomic.AddInt32(&callCount, 1)
		mu.Lock()
		gotApp, gotSocket = app, socket
		mu.Unlock()
		return nil
	}

	at, _ := newEmberTestAgentTab(t, config.OverlayConfig{Spawn: emberBoolPtr(false)})

	handled, _ := at.handleSlashCommand("/ember")
	require.True(t, handled, "/ember must be a recognised slash command")

	assert.Eventually(t, func() bool {
		return atomic.LoadInt32(&callCount) >= 1
	}, 500*time.Millisecond, 20*time.Millisecond,
		"overlayLauncher must be invoked when /ember activates with Spawn=false")

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, gotApp, "Providence Overlay.app", "launcher must receive the bundle path")
	assert.Contains(t, gotSocket, "overlay.sock", "launcher must receive the UDS socket path")
}

// TestEmberActivationSkipsOverlayLauncherWhenSpawnEnabled verifies the
// launcher is NOT invoked when Spawn is enabled (default). In that mode
// the manager forks the overlay itself via Manager.Start, so the
// bundle-open fallback would be a double-launch.
func TestEmberActivationSkipsOverlayLauncherWhenSpawnEnabled(t *testing.T) {
	orig := overlayLauncher
	t.Cleanup(func() { overlayLauncher = orig })

	var callCount int32
	overlayLauncher = func(_, _ string) error {
		atomic.AddInt32(&callCount, 1)
		return nil
	}

	at, _ := newEmberTestAgentTab(t, config.OverlayConfig{Spawn: emberBoolPtr(true)})

	handled, _ := at.handleSlashCommand("/ember")
	require.True(t, handled)

	// Give the goroutines a chance to misfire.
	time.Sleep(250 * time.Millisecond)
	assert.Zero(t, atomic.LoadInt32(&callCount),
		"overlayLauncher must not run when Spawn is enabled - manager owns the fork")
}

// TestEmberActivationKickstartFiresFirstTick verifies the 150ms kickstart
// goroutine invokes engine.Send with a well-formed tick string so the
// model wakes up immediately instead of waiting for the next user turn.
func TestEmberActivationKickstartFiresFirstTick(t *testing.T) {
	at, eng := newEmberTestAgentTab(t, config.OverlayConfig{Spawn: emberBoolPtr(true)})

	handled, _ := at.handleSlashCommand("/ember")
	require.True(t, handled)

	assert.Eventually(t, func() bool {
		return len(eng.SentMessages()) >= 1
	}, 600*time.Millisecond, 20*time.Millisecond,
		"kickstart must invoke engine.Send once within ~150ms of activation")

	msgs := eng.SentMessages()
	require.NotEmpty(t, msgs)
	assert.Contains(t, msgs[0], "<tick>",
		"kickstart payload must be an ember tick envelope so the model routes it correctly")
}

// TestEmberActivationKickstartSkippedWhenEngineNil verifies the kickstart
// goroutine's nil-engine guard. An AgentTab with ember wired but engine
// absent must not panic on /ember and must record no tick.
func TestEmberActivationKickstartSkippedWhenEngineNil(t *testing.T) {
	cfg := config.Config{Overlay: config.OverlayConfig{Spawn: emberBoolPtr(true)}}
	at := NewAgentTab("", cfg, nil, nil)
	at.ember = ember.New()
	// Deliberately leave at.engine == nil.

	handled, _ := at.handleSlashCommand("/ember")
	require.True(t, handled)

	// Nothing observable should happen; just wait past the kickstart
	// window and confirm we did not blow up.
	time.Sleep(250 * time.Millisecond)
}
