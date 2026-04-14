//go:build darwin

package macos

import (
	"context"
	"encoding/json"
	"fmt"
)

// Permission identifies a macOS permission required by the bridge.
type Permission int

const (
	// PermScreenRecording is the Screen Recording permission.
	PermScreenRecording Permission = iota
	// PermAccessibility is the Accessibility permission.
	PermAccessibility
)

// String returns the human-readable name of the permission.
func (p Permission) String() string {
	switch p {
	case PermScreenRecording:
		return "screen_recording"
	case PermAccessibility:
		return "accessibility"
	default:
		return fmt.Sprintf("permission(%d)", int(p))
	}
}

// PermissionStatus is the current status of a single macOS permission.
type PermissionStatus struct {
	Permission  Permission
	Granted     bool
	Prompted    bool
	SettingsURL string
	Hint        string
}

// PermissionDeniedError is returned when a required macOS permission is not granted.
type PermissionDeniedError struct {
	Permission  Permission
	SettingsURL string
	Hint        string
}

// Error implements error.
func (e *PermissionDeniedError) Error() string {
	msg := fmt.Sprintf("permission denied: %s", e.Permission)
	if e.Hint != "" {
		msg += " - " + e.Hint
	}
	if e.SettingsURL != "" {
		msg += " (" + e.SettingsURL + ")"
	}
	return msg
}

// preflightRaw is the wire shape returned by the Swift preflight RPC.
type preflightRaw struct {
	Permissions []preflightEntry `json:"permissions"`
}

type preflightEntry struct {
	Permission  string `json:"permission"`
	Granted     bool   `json:"granted"`
	Prompted    bool   `json:"prompted,omitempty"`
	SettingsURL string `json:"settings_url,omitempty"`
	Hint        string `json:"hint,omitempty"`
}

// Preflight calls the Swift bridge's preflight RPC and returns the parsed
// permission statuses. Returns an error if the Swift bridge is unavailable.
func (b *Bridge) Preflight(ctx context.Context) ([]PermissionStatus, error) {
	b.mu.Lock()
	swift := b.swift
	b.mu.Unlock()

	if swift == nil {
		// Try to spawn first.
		if !b.useSwift(CapScreenshot) {
			return nil, fmt.Errorf("swift bridge unavailable: %w", b.spawnErr)
		}
		b.mu.Lock()
		swift = b.swift
		b.mu.Unlock()
	}

	if swift == nil {
		return nil, fmt.Errorf("swift bridge unavailable")
	}

	raw, err := swift.call(ctx, "preflight", nil)
	if err != nil {
		return nil, fmt.Errorf("preflight rpc: %w", err)
	}

	var result preflightRaw
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("preflight: decode response: %w", err)
	}

	statuses := make([]PermissionStatus, 0, len(result.Permissions))
	for _, e := range result.Permissions {
		perm := parsePermission(e.Permission)
		statuses = append(statuses, PermissionStatus{
			Permission:  perm,
			Granted:     e.Granted,
			Prompted:    e.Prompted,
			SettingsURL: e.SettingsURL,
			Hint:        e.Hint,
		})
	}
	return statuses, nil
}

// parsePermission converts a wire permission string to a typed Permission.
func parsePermission(s string) Permission {
	switch s {
	case "screen_recording":
		return PermScreenRecording
	case "accessibility":
		return PermAccessibility
	default:
		return PermScreenRecording // default to first for unknown values
	}
}
