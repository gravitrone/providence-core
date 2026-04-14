//go:build !darwin

// Package macos provides a stub on non-darwin platforms so that cross-platform
// builds can import the package without linker errors. All types are defined;
// functionality is unavailable.
package macos

import (
	"context"
	"errors"
	"time"
)

// errUnsupported is returned by all bridge methods on non-darwin platforms.
var errUnsupported = errors.New("bridge: macOS only")

// Metrics is a no-op metrics tracker on non-darwin platforms.
type Metrics struct{}

// NewMetrics returns a no-op Metrics.
func NewMetrics() *Metrics { return &Metrics{} }

// Record is a no-op on non-darwin platforms.
func (m *Metrics) Record(_ string, _ time.Duration, _ bool) {}

// Snapshot returns an empty map on non-darwin platforms.
func (m *Metrics) Snapshot() map[string]HistogramSnapshot { return nil }

// HistogramSnapshot holds per-op latency percentiles.
type HistogramSnapshot struct {
	Op         string        `json:"op"`
	Count      int64         `json:"count"`
	ErrorCount int64         `json:"error_count"`
	P50        time.Duration `json:"p50_ns"`
	P95        time.Duration `json:"p95_ns"`
	P99        time.Duration `json:"p99_ns"`
	Max        time.Duration `json:"max_ns"`
}

// Permission identifies a macOS permission required by the bridge.
type Permission int

const (
	// PermScreenRecording is the Screen Recording permission.
	PermScreenRecording Permission = iota
	// PermAccessibility is the Accessibility permission.
	PermAccessibility
)

// String returns the permission name.
func (p Permission) String() string { return "unsupported" }

// PermissionStatus is a stub on non-darwin platforms.
type PermissionStatus struct {
	Permission  Permission
	Granted     bool
	Prompted    bool
	SettingsURL string
	Hint        string
}

// ErrPermissionDenied is a stub on non-darwin platforms.
type ErrPermissionDenied struct {
	Permission  Permission
	SettingsURL string
	Hint        string
}

// Error implements error.
func (e *ErrPermissionDenied) Error() string { return errUnsupported.Error() }

// Bridge is a no-op stub on non-darwin platforms.
type Bridge struct{}

// Option configures a Bridge.
type Option func(*Bridge)

// New returns a stub Bridge.
func New(_ ...Option) *Bridge { return &Bridge{} }

// Metrics returns a no-op metrics tracker.
func (b *Bridge) Metrics() *Metrics { return NewMetrics() }

// Preflight returns an error on non-darwin platforms.
func (b *Bridge) Preflight(_ context.Context) ([]PermissionStatus, error) {
	return nil, errUnsupported
}

// BridgeConfig is a stub type on non-darwin platforms.
// It is declared here so WithConfig can be compiled cross-platform.
// The actual config type lives in internal/config; this alias exists only
// to satisfy callers that import bridge/macos directly.
// (No-op: callers should use config.BridgeConfig directly.)

// WithMode is a no-op on non-darwin platforms.
func WithMode(_ string) Option { return func(*Bridge) {} }

// WithLogger is a no-op on non-darwin platforms.
func WithLogger(_ interface{}) Option { return func(*Bridge) {} }
