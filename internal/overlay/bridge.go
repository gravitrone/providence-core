package overlay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/gravitrone/providence-core/internal/engine/ember"
	"github.com/gravitrone/providence-core/internal/engine/session"
)

// ScreenshotFrame is a decoded PNG observation kept in the bridge ring buffer
// for vision attachment on the next model turn.
type ScreenshotFrame struct {
	Time time.Time
	PNG  []byte
}

// screenRingCap is how many frames the bridge retains at once. With a 5s
// capture cadence that's a 30s rolling window.
const screenRingCap = 6

// screenFrameTTL evicts ring entries older than this on read. Prevents a long
// idle from leaking ancient frames into the next model turn.
const screenFrameTTL = 30 * time.Second

// --- Engine Surface ---

// Engine is the minimum engine surface the overlay bridge needs.
// It is intentionally narrow to avoid coupling the overlay package to a
// specific engine implementation.
//
// NOTE: the existing engine.Engine interface does not yet expose SessionID()
// or a context-aware Send(ctx, text). These are follow-up tasks for Phase 7.
// For Phase 6, SessionID is sourced from the bridge directly via SetSessionID,
// and Send uses context.Background() internally.
type Engine interface {
	// Send sends a user message to the engine.
	Send(text string) error
	// Interrupt aborts the current in-flight turn.
	Interrupt()
	// Model returns the current model identifier.
	Model() string
	// EngineType returns the engine backend name (e.g. "claude").
	EngineType() string
	// SessionBus returns the engine's session event bus.
	SessionBus() *session.Bus
}

// --- Bridge ---

// Bridge plumbs events between the engine/ember and connected overlay clients.
// It implements ServerHandler so it can be wired directly into the UDS server.
type Bridge struct {
	engine    Engine
	ember     *ember.State
	server    *Server
	manager   *Manager // optional - for marking hello
	logger    *slog.Logger
	sessionID string

	// injection is preserved as a config field for backward compat but the
	// post-rewire pipeline always uses ring-buffered image attachment + the
	// rolling transcript on the next natural turn / ember tick. The old
	// "synthetic_user" forced-send mode is no longer fired.
	injection string

	tracker *TokenTracker

	// engineMu guards late-wire of engine via SetEngine.
	engineMu sync.RWMutex
	// serverMu guards late-wire of server via SetServer.
	serverMu sync.RWMutex

	// TUI-side runtime prefs forwarded to the overlay in Welcome. Now reduced
	// to TTS-only after the ambient rewire dropped position/exclusion/UI fields.
	prefsMu    sync.RWMutex
	ttsEnabled bool

	// screenRing holds the last N decoded screenshots from the overlay. Read
	// at model turn time and attached as image content blocks. Independently
	// guarded from the prefs lock to avoid contention during burst updates.
	ringMu            sync.RWMutex
	screenRing        []ScreenshotFrame
	latestTranscript  string
	lastAttachedFirst time.Time // dedup: skip image attach if ring unchanged

	// Phase G: daily budget bookkeeping. budgetWarnedDay holds the start-of-day
	// marker for the last date we logged the "budget exceeded" warning, so we
	// warn at most once per local day instead of spamming on every update.
	budgetMu        sync.Mutex
	budgetWarnedDay time.Time
}

// NewBridge creates a Bridge connecting the engine and ember state to the
// overlay server. manager may be nil; when set it is used to mark hello
// so the manager's Start polling loop unblocks.
func NewBridge(eng Engine, em *ember.State, server *Server, manager *Manager, logger *slog.Logger) *Bridge {
	if logger == nil {
		logger = slog.Default()
	}
	b := &Bridge{
		engine:    eng,
		ember:     em,
		server:    server,
		manager:   manager,
		logger:    logger,
		injection: "system_reminder",
		tracker:   NewTokenTracker(),
	}
	return b
}

// NewBridgeWithMode is like NewBridge but lets callers select the context
// injection mode at construction time. Empty mode defaults to system_reminder.
func NewBridgeWithMode(eng Engine, em *ember.State, server *Server, manager *Manager, logger *slog.Logger, mode string) *Bridge {
	b := NewBridge(eng, em, server, manager, logger)
	if mode == "" {
		mode = "system_reminder"
	}
	b.injection = mode
	return b
}

// Tracker returns the per-session token tracker. Never nil for a Bridge
// constructed via NewBridge / NewBridgeWithMode.
func (b *Bridge) Tracker() *TokenTracker { return b.tracker }

// SetDailyBudget configures the per-day token breaker on the underlying
// tracker. 0 disables gating. Phase G.
func (b *Bridge) SetDailyBudget(limit int) {
	if b == nil || b.tracker == nil {
		return
	}
	b.tracker.SetDailyBudget(limit)
}

// InjectionMode returns the configured injection mode.
func (b *Bridge) InjectionMode() string { return b.injection }

// SetServer wires a server after construction (used when the server is
// created by the manager at Start time).
func (b *Bridge) SetServer(srv *Server) {
	b.serverMu.Lock()
	defer b.serverMu.Unlock()
	b.server = srv
}

// SetSessionID sets the session ID to include in Welcome messages.
func (b *Bridge) SetSessionID(id string) { b.sessionID = id }

// SetEngine attaches the engine after construction. Used by the TUI to wire
// the engine reference after both the bridge and engine are initialized.
// Nil-safe: a nil engine means Welcome responses include empty engine/model.
func (b *Bridge) SetEngine(eng Engine) {
	b.engineMu.Lock()
	defer b.engineMu.Unlock()
	b.engine = eng
}

// getServer returns the current server under the read lock.
func (b *Bridge) getServer() *Server {
	b.serverMu.RLock()
	defer b.serverMu.RUnlock()
	return b.server
}

// getEngine returns the current engine under the read lock.
func (b *Bridge) getEngine() Engine {
	b.engineMu.RLock()
	defer b.engineMu.RUnlock()
	return b.engine
}

// SetRuntimePrefs records TUI-side preferences advertised to the overlay in
// each Welcome envelope. Post-ambient-rewire this is just TTS - all the UI
// position / exclusion / chat-rendering knobs are local Swift UserDefaults.
func (b *Bridge) SetRuntimePrefs(tts bool) {
	b.prefsMu.Lock()
	b.ttsEnabled = tts
	b.prefsMu.Unlock()
}

// ScreenshotPNGs returns just the decoded PNG byte slices from the ring,
// oldest first. Convenience for callers (engine) that don't need timestamps.
func (b *Bridge) ScreenshotPNGs() [][]byte {
	frames := b.Screenshots()
	if len(frames) == 0 {
		return nil
	}
	out := make([][]byte, len(frames))
	for i, f := range frames {
		out[i] = f.PNG
	}
	return out
}

// Screenshots returns a copy of the current ring buffer of decoded PNG frames,
// oldest first. Frames older than screenFrameTTL are evicted at read time.
// Returns nil if the ring is empty.
func (b *Bridge) Screenshots() []ScreenshotFrame {
	b.ringMu.Lock()
	defer b.ringMu.Unlock()
	cutoff := time.Now().Add(-screenFrameTTL)
	live := b.screenRing[:0]
	for _, f := range b.screenRing {
		if f.Time.After(cutoff) {
			live = append(live, f)
		}
	}
	b.screenRing = live
	if len(live) == 0 {
		return nil
	}
	out := make([]ScreenshotFrame, len(live))
	copy(out, live)
	return out
}

// Transcript returns the current rolling transcript snapshot (may be empty).
func (b *Bridge) Transcript() string {
	b.ringMu.RLock()
	defer b.ringMu.RUnlock()
	return b.latestTranscript
}

// MarkAttached records that the engine attached the current ring head; used
// for skip-on-no-change deduping at Send time.
func (b *Bridge) MarkAttached() {
	b.ringMu.Lock()
	defer b.ringMu.Unlock()
	if len(b.screenRing) > 0 {
		b.lastAttachedFirst = b.screenRing[0].Time
	}
}

// RingChangedSinceLastAttach returns true if the ring head differs from what
// was last attached. Caller-side opportunistic skip during idle ember ticks.
func (b *Bridge) RingChangedSinceLastAttach() bool {
	b.ringMu.RLock()
	defer b.ringMu.RUnlock()
	if len(b.screenRing) == 0 {
		return false
	}
	return !b.screenRing[0].Time.Equal(b.lastAttachedFirst)
}

// Start subscribes to the session bus and forwards events to connected
// overlays. It runs until ctx is cancelled.
func (b *Bridge) Start(ctx context.Context) {
	eng := b.getEngine()
	if eng == nil {
		return
	}
	bus := eng.SessionBus()
	if bus == nil {
		return
	}
	ch := bus.Subscribe(32)
	defer bus.Unsubscribe(ch)

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			b.forwardSessionEvent(ev)
		}
	}
}

// PendingSystemReminder returns the current rolling transcript wrapped as a
// system-reminder block, or "" if no transcript exists. Unlike pre-rewire,
// this no longer carries activity/OCR/AX text - those came from the deleted
// Swift heuristic layer. Image attachments handle the rest of the context.
// Non-destructive read: ember ticks may pull the same transcript repeatedly
// if no new audio has arrived.
func (b *Bridge) PendingSystemReminder() string {
	b.ringMu.RLock()
	tx := b.latestTranscript
	b.ringMu.RUnlock()
	return formatTranscriptReminder(tx)
}

// --- ServerHandler implementation ---

// OnHello handles the overlay's initial Hello and returns a Welcome.
func (b *Bridge) OnHello(c *client, h Hello) Welcome {
	b.logger.Info("overlay: hello received",
		"pid", h.PID,
		"version", h.ClientVersion,
		"capabilities", h.Capabilities,
	)

	cwd, _ := os.Getwd()

	w := Welcome{
		SessionID: b.sessionID,
		CWD:       cwd,
		Timestamp: time.Now(),
	}

	if eng := b.getEngine(); eng != nil {
		w.Engine = eng.EngineType()
		w.Model = eng.Model()
	}
	if b.ember != nil {
		w.EmberActive = b.ember.ShouldTick()
	}

	b.prefsMu.RLock()
	w.TTSEnabled = b.ttsEnabled
	b.prefsMu.RUnlock()

	return w
}

// OnContextUpdate receives a screen frame and/or transcript snapshot from the
// overlay. Frames push into the ring buffer for vision attachment on the next
// model turn. Transcript replaces the current snapshot. Token bookkeeping +
// daily budget breaker enforce cost ceilings.
func (b *Bridge) OnContextUpdate(_ *client, u ContextUpdate) error {
	mode := b.injection
	if mode == "" {
		mode = "system_reminder"
	}

	// Decode + push screenshot into the ring (if this update carries one).
	var pngBytes []byte
	if u.ScreenshotPNGB64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(u.ScreenshotPNGB64)
		if err == nil {
			pngBytes = decoded
		} else {
			b.logger.Debug("overlay: bad screenshot base64", "error", err, "len", len(u.ScreenshotPNGB64))
		}
	}

	// Token cost estimate: sum image + transcript tokens.
	tokens := 0
	if pngBytes != nil {
		tokens += EstimateImageTokens(pngBytes)
	}
	if u.Transcript != "" {
		tokens += EstimateTokens(u.Transcript)
	}

	// Daily budget breaker. Check BEFORE recording so an already-exceeded
	// budget doesn't keep inflating the counter. Still broadcast an ack with
	// budget_exceeded reason so the overlay can show a muted indicator.
	if b.tracker != nil && b.tracker.BudgetExceeded() {
		b.warnBudgetOncePerDay()
		if srv := b.getServer(); srv != nil {
			ack := ContextAck{
				Tokens: 0,
				Reason: "budget_exceeded",
				Mode:   mode,
				Total:  b.tracker.Total(),
			}
			if err := srv.Broadcast(TypeContextAck, ack); err != nil {
				b.logger.Debug("overlay: broadcast context_ack failed", "error", err)
			}
		}
		return nil
	}

	// Push into the ring. Frames stored even when over budget would bypass
	// future caps; we already returned above in that case.
	b.ringMu.Lock()
	if pngBytes != nil {
		b.screenRing = append(b.screenRing, ScreenshotFrame{Time: time.Now(), PNG: pngBytes})
		if len(b.screenRing) > screenRingCap {
			// Drop oldest. Simple slice shift; ring is small (cap=6).
			b.screenRing = b.screenRing[len(b.screenRing)-screenRingCap:]
		}
	}
	if u.Transcript != "" {
		b.latestTranscript = u.Transcript
	}
	b.ringMu.Unlock()

	if b.tracker != nil && tokens > 0 {
		b.tracker.Record(TokenEntry{
			Time:   time.Now(),
			Tokens: tokens,
			Reason: u.ChangeKind,
			App:    "overlay",
			Mode:   mode,
		})
	}

	// synthetic_user mode is now a no-op for ambient observations - the model
	// sees frames + transcript via the next natural turn (or ember tick) via
	// the contextInjector.Screenshots/Transcript path. Keep the field on the
	// struct for backward-compat config but stop firing direct sends.

	if srv := b.getServer(); srv != nil {
		ack := ContextAck{
			Tokens: tokens,
			Reason: u.ChangeKind,
			Mode:   mode,
			Total:  b.tracker.Total(),
		}
		if err := srv.Broadcast(TypeContextAck, ack); err != nil {
			b.logger.Debug("overlay: broadcast context_ack failed", "error", err)
		}
	}
	return nil
}

// warnBudgetOncePerDay emits a single Warn log per local day when the daily
// budget breaker trips. Subsequent trips on the same day are silent to avoid
// log spam; the next calendar day re-arms the warning.
func (b *Bridge) warnBudgetOncePerDay() {
	now := time.Now()
	sd := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	b.budgetMu.Lock()
	warned := !b.budgetWarnedDay.Before(sd)
	if !warned {
		b.budgetWarnedDay = sd
	}
	b.budgetMu.Unlock()
	if !warned {
		budget := 0
		if b.tracker != nil {
			budget = b.tracker.DailyBudget()
		}
		b.logger.Warn("overlay: daily token budget exceeded, skipping injection",
			"budget", budget)
	}
}

// OnUserQuery forwards the user's overlay query to the engine.
func (b *Bridge) OnUserQuery(_ *client, q UserQuery) error {
	eng := b.getEngine()
	if eng == nil {
		return fmt.Errorf("overlay: no engine available")
	}
	b.logger.Debug("overlay: user query", "source", q.Source, "text_len", len(q.Text))
	return eng.Send(q.Text)
}

// OnEmberRequest handles requests to change the ember autonomous mode state.
func (b *Bridge) OnEmberRequest(_ *client, r EmberRequest) error {
	if b.ember == nil {
		return fmt.Errorf("overlay: no ember state available")
	}
	switch r.Desired {
	case "active":
		b.ember.Activate()
	case "inactive":
		b.ember.Deactivate()
	case "paused":
		b.ember.Pause()
	case "resumed":
		b.ember.Resume()
	default:
		return fmt.Errorf("overlay: unknown ember desired state %q", r.Desired)
	}

	// Broadcast the updated state to all connected clients.
	if srv := b.getServer(); srv != nil {
		_ = srv.Broadcast(TypeEmberState, EmberState{
			Active: b.ember.Active,
			Paused: b.ember.Paused,
		})
	}
	return nil
}

// OnInterrupt forwards an interrupt signal to the engine.
func (b *Bridge) OnInterrupt(_ *client) error {
	eng := b.getEngine()
	if eng == nil {
		return fmt.Errorf("overlay: no engine available")
	}
	eng.Interrupt()
	return nil
}

// OnUIEvent handles overlay UI telemetry. Currently a no-op for Phase 6.
func (b *Bridge) OnUIEvent(_ *client, e UIEvent) error {
	b.logger.Debug("overlay: ui event", "kind", e.Kind, "target", e.Target)
	return nil
}

// OnDisconnect logs when a client disconnects.
func (b *Bridge) OnDisconnect(_ *client) {
	b.logger.Info("overlay: client disconnected")
}

// --- Internal ---

// forwardSessionEvent serialises a session.Event and broadcasts it to
// all connected overlay clients.
func (b *Bridge) forwardSessionEvent(ev session.Event) {
	srv := b.getServer()
	if srv == nil {
		return
	}
	rawData, err := json.Marshal(ev.Data)
	if err != nil {
		b.logger.Debug("overlay: marshal session event data failed", "error", err)
		rawData = json.RawMessage(`null`)
	}
	se := SessionEvent{
		Type: ev.Type,
		Data: rawData,
	}
	if err := srv.Broadcast(TypeSessionEvent, se); err != nil {
		b.logger.Debug("overlay: broadcast session event failed", "error", err)
	}
}

// formatTranscriptReminder produces a tiny text reminder to accompany the
// image attachments. Used by PendingSystemReminder so the model sees a
// human-readable transcript header next to the visual frames.
func formatTranscriptReminder(transcript string) string {
	if transcript == "" {
		return ""
	}
	return fmt.Sprintf(
		"<system-reminder origin=\"overlay\">\nRecent speech (last ~30s): %s\n</system-reminder>",
		transcript,
	)
}
