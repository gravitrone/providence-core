package permissions

import "sync"

// --- Auto-Approval Mode ---

// autoApproveTools is the allowlist of read-only tool names that can be
// auto-approved when autoApprove mode is enabled. Writes, Bash, and Edit
// never qualify, even with autoApprove on.
var autoApproveTools = map[string]bool{
	"Read": true,
	"Grep": true,
	"Glob": true,
}

// AutoMode is a session-scoped toggle that auto-approves read-only tools.
// Entered via explicit user action (config or runtime UI). State is not
// persisted to disk: a new session always starts with autoApprove off.
type AutoMode struct {
	mu      sync.RWMutex
	enabled bool
}

// NewAutoMode constructs a disabled auto-mode tracker.
func NewAutoMode() *AutoMode {
	return &AutoMode{}
}

// SetAutoMode toggles auto-approval on or off for the current session.
func (a *AutoMode) SetAutoMode(enabled bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.enabled = enabled
}

// Enabled reports whether auto-approval is currently on.
func (a *AutoMode) Enabled() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.enabled
}

// IsAutoApproved returns true when the tool invocation qualifies for
// auto-approval: auto-mode must be on AND the tool must be in the read-only
// allowlist. Safety paths still block auto-approval so the user keeps a
// chance to veto writes that touch protected locations.
func (a *AutoMode) IsAutoApproved(tool string, input interface{}) bool {
	if !a.Enabled() {
		return false
	}
	if !autoApproveTools[tool] {
		return false
	}
	if isSafetyPath(tool, input) {
		return false
	}
	return true
}
