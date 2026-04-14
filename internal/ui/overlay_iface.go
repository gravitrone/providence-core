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
type overlayBridge interface {
	overlay.ServerHandler
}

// WithOverlay is a functional option for wiring an overlay manager and bridge
// into the AgentTab after construction.
func WithOverlay(mgr overlayManager, bridge overlayBridge) func(*AgentTab) {
	return func(at *AgentTab) {
		at.overlayMgr = mgr
		at.overlayBridge = bridge
	}
}
