package ui

import (
	"context"

	"github.com/gravitrone/providence-core/internal/overlay"
)

// overlayManager is the subset of overlay.Manager used by the UI slash commands.
// Defined here so agent_tab.go can reference it without the full overlay package
// polluting the import graph of tests.
type overlayManager interface {
	Start(ctx context.Context, handler overlay.ServerHandler) error
	Stop(ctx context.Context) error
	StatusInfo() map[string]any
}

// overlayBridge is the subset of overlay.Bridge used by the UI slash commands.
// It includes overlay.Injector so the engine can be wired for context injection.
type overlayBridge interface {
	overlay.ServerHandler
	overlay.Injector
}

// overlayTracked is optionally implemented by an overlayBridge that exposes a
// TokenTracker, used by `/overlay cost`. Kept as a separate interface so tests
// can substitute a minimal bridge without needing a tracker.
type overlayTracked interface {
	Tracker() *overlay.TokenTracker
}

// overlayPrefsReader is optionally implemented by an overlayBridge that
// advertises runtime prefs to overlay clients. Used by `/overlay status`.
// Phase 10.
type overlayPrefsReader interface {
	RuntimePrefs() (tts bool, position string, excludedApps []string)
}

// WithOverlay is a functional option for wiring an overlay manager and bridge
// into the AgentTab after construction.
func WithOverlay(mgr overlayManager, bridge overlayBridge) func(*AgentTab) {
	return func(at *AgentTab) {
		at.overlayMgr = mgr
		at.overlayBridge = bridge
	}
}
