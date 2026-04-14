package overlay

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/gravitrone/providence-core/internal/engine/ember"
	"github.com/gravitrone/providence-core/internal/engine/session"
)

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

	// injection selects how context_update payloads reach the engine.
	// "system_reminder" (default): store as pending reminder, prepended to
	// the next user turn. "synthetic_user": call engine.Send directly with
	// the formatted reminder as the user message, rate-limited to 1 per 10s.
	injection       string
	lastSyntheticAt time.Time
	syntheticMu     sync.Mutex

	tracker *TokenTracker

	// pendingReminder holds the latest ContextUpdate formatted as a
	// <system-reminder> block. Consumed once by PendingSystemReminder.
	pendingMu       sync.Mutex
	pendingReminder string

	// Phase 10: runtime prefs forwarded to the overlay in Welcome.
	prefsMu      sync.RWMutex
	ttsEnabled   bool
	position     string
	excludedApps []string
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

// InjectionMode returns the configured injection mode.
func (b *Bridge) InjectionMode() string { return b.injection }

// SetServer wires a server after construction (used when the server is
// created by the manager at Start time).
func (b *Bridge) SetServer(srv *Server) { b.server = srv }

// SetSessionID sets the session ID to include in Welcome messages.
func (b *Bridge) SetSessionID(id string) { b.sessionID = id }

// SetRuntimePrefs records TUI-side preferences that are advertised to the
// overlay client in each Welcome envelope. Safe to call at any time; takes
// effect on the next hello exchange. Phase 10.
func (b *Bridge) SetRuntimePrefs(tts bool, position string, excludedApps []string) {
	b.prefsMu.Lock()
	b.ttsEnabled = tts
	b.position = position
	// Copy to avoid the caller mutating our internal slice.
	if len(excludedApps) > 0 {
		b.excludedApps = append([]string(nil), excludedApps...)
	} else {
		b.excludedApps = nil
	}
	b.prefsMu.Unlock()
}

// RuntimePrefs returns a snapshot of the TUI-side runtime preferences. Phase 10.
func (b *Bridge) RuntimePrefs() (tts bool, position string, excludedApps []string) {
	b.prefsMu.RLock()
	defer b.prefsMu.RUnlock()
	tts = b.ttsEnabled
	position = b.position
	if len(b.excludedApps) > 0 {
		excludedApps = append([]string(nil), b.excludedApps...)
	}
	return
}

// Start subscribes to the session bus and forwards events to connected
// overlays. It runs until ctx is cancelled.
func (b *Bridge) Start(ctx context.Context) {
	if b.engine == nil {
		return
	}
	bus := b.engine.SessionBus()
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

// PendingSystemReminder returns the most recently received ContextUpdate
// formatted as a <system-reminder> block, and clears it. Returns "" if no
// update is pending.
func (b *Bridge) PendingSystemReminder() string {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()
	s := b.pendingReminder
	b.pendingReminder = ""
	return s
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

	if b.engine != nil {
		w.Engine = b.engine.EngineType()
		w.Model = b.engine.Model()
	}
	if b.ember != nil {
		w.EmberActive = b.ember.ShouldTick()
	}

	b.prefsMu.RLock()
	w.TTSEnabled = b.ttsEnabled
	w.Position = b.position
	if len(b.excludedApps) > 0 {
		w.ExcludedApps = append([]string(nil), b.excludedApps...)
	}
	b.prefsMu.RUnlock()

	return w
}

// OnContextUpdate receives a screen/audio observation from the overlay.
//
// Routing depends on b.injection:
//   - "system_reminder" (default): stores the formatted reminder for the next
//     engine turn (the engine prepends it to the user message).
//   - "synthetic_user": calls engine.Send directly with the reminder, so the
//     model narrates in real time. Rate-limited to 1 send per 10s; when the
//     rate limit kicks in we fall back to storing the reminder.
//
// On every accepted update we record the approximate token count in the
// session TokenTracker and broadcast a ContextAck to connected clients.
func (b *Bridge) OnContextUpdate(_ *client, u ContextUpdate) error {
	reminder := formatContextReminder(u)
	tokens := EstimateTokens(reminder)

	mode := b.injection
	if mode == "" {
		mode = "system_reminder"
	}

	if b.tracker != nil {
		b.tracker.Record(TokenEntry{
			Time:   time.Now(),
			Tokens: tokens,
			Reason: u.ChangeKind,
			App:    u.ActiveApp,
			Mode:   mode,
		})
	}

	switch mode {
	case "synthetic_user":
		b.syntheticMu.Lock()
		canSend := time.Since(b.lastSyntheticAt) >= 10*time.Second
		if canSend {
			b.lastSyntheticAt = time.Now()
		}
		b.syntheticMu.Unlock()

		if canSend && b.engine != nil {
			// Fire-and-forget: the engine will stream a response on its own
			// loop. A failed Send should not take down the bridge.
			go func(text string) {
				defer func() {
					if r := recover(); r != nil {
						b.logger.Warn("overlay: synthetic send panic recovered", "panic", r)
					}
				}()
				if err := b.engine.Send(text); err != nil {
					b.logger.Warn("overlay: synthetic send failed", "error", err)
				}
			}(reminder)
		} else {
			// Rate-limited or no engine - fall back to reminder store so the
			// next natural turn still sees the update.
			b.storeAsReminder(reminder)
		}
	default:
		b.storeAsReminder(reminder)
	}

	if b.server != nil {
		ack := ContextAck{
			Tokens: tokens,
			Reason: u.ChangeKind,
			Mode:   mode,
			Total:  b.tracker.Total(),
		}
		if err := b.server.Broadcast(TypeContextAck, ack); err != nil {
			b.logger.Debug("overlay: broadcast context_ack failed", "error", err)
		}
	}
	return nil
}

// storeAsReminder saves the formatted reminder for later PendingSystemReminder.
func (b *Bridge) storeAsReminder(text string) {
	b.pendingMu.Lock()
	b.pendingReminder = text
	b.pendingMu.Unlock()
}

// OnUserQuery forwards the user's overlay query to the engine.
func (b *Bridge) OnUserQuery(_ *client, q UserQuery) error {
	if b.engine == nil {
		return fmt.Errorf("overlay: no engine available")
	}
	b.logger.Debug("overlay: user query", "source", q.Source, "text_len", len(q.Text))
	return b.engine.Send(q.Text)
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
	if b.server != nil {
		_ = b.server.Broadcast(TypeEmberState, EmberState{
			Active: b.ember.Active,
			Paused: b.ember.Paused,
		})
	}
	return nil
}

// OnInterrupt forwards an interrupt signal to the engine.
func (b *Bridge) OnInterrupt(_ *client) error {
	if b.engine == nil {
		return fmt.Errorf("overlay: no engine available")
	}
	b.engine.Interrupt()
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
	if b.server == nil {
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
	if err := b.server.Broadcast(TypeSessionEvent, se); err != nil {
		b.logger.Debug("overlay: broadcast session event failed", "error", err)
	}
}

// formatContextReminder wraps a ContextUpdate as a <system-reminder> block
// suitable for injection into the engine as a system message.
// The origin="overlay" attribute signals the model that this is observational
// screen context, not user input.
func formatContextReminder(u ContextUpdate) string {
	ts := u.Timestamp.Format("15:04:05")
	if u.Timestamp.IsZero() {
		ts = time.Now().Format("15:04:05")
	}

	activity := u.Activity
	if activity == "" {
		activity = "general"
	}

	appInfo := u.ActiveApp
	if u.WindowTitle != "" {
		appInfo = u.ActiveApp + " - " + u.WindowTitle
	}

	var axLine string
	if u.AXSummary != "" {
		axLine = "\nAX: " + u.AXSummary
	}

	transcript := "(silent)"
	if u.Transcript != "" {
		transcript = u.Transcript
	}

	var ocrLine string
	if u.OCRText != "" {
		ocrLine = "\nOCR: " + u.OCRText
	}

	var deltaLine string
	if u.ChangeKind != "" {
		deltaLine = "\nDelta: " + u.ChangeKind
	}

	return fmt.Sprintf(
		"<system-reminder origin=\"overlay\">\n# Screen Context (as of %s)\nActive: %s (%s)%s%s\nRecent speech: %s%s\n</system-reminder>",
		ts, appInfo, activity, axLine, ocrLine, transcript, deltaLine,
	)
}

