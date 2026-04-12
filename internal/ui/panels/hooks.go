package panels

import (
	"fmt"
	"strings"
)

// HookInfo represents an active hook configuration.
type HookInfo struct {
	Event     string
	LastFired string // "0.2s", empty if never
}

// RenderHooks shows active hook firings.
func RenderHooks(hooks []HookInfo, width int) string {
	if len(hooks) == 0 {
		return "  No hooks configured"
	}
	var lines []string
	for _, h := range hooks {
		status := "idle"
		if h.LastFired != "" {
			status = "fired " + h.LastFired
		}
		lines = append(lines, fmt.Sprintf("  %-18s %s", h.Event, status))
	}
	return strings.Join(lines, "\n")
}
