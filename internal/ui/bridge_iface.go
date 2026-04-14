package ui

import (
	"context"

	"github.com/gravitrone/providence-core/internal/bridge/macos"
)

// bridgeProvider is the subset of macos.Bridge used by the UI slash commands.
// Defined here so agent_tab.go can reference it without a darwin build tag.
// On non-darwin platforms the field will always be nil.
type bridgeProvider interface {
	Metrics() *macos.Metrics
	Preflight(ctx context.Context) ([]macos.PermissionStatus, error)
}
