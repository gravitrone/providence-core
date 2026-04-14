//go:build darwin

package macos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

var (
	osExecutableFunc      = os.Executable
	swiftGlobalInstallDir = "/usr/local/lib/providence/providence-mac-bridge"
)

// Bridge provides macOS computer use capabilities with Swift-first fallback behavior.
type Bridge struct {
	mode         string
	swift        *swiftClient
	shell        *shellClient
	caps         map[Capability]bool
	mu           sync.Mutex
	spawnOnce    sync.Once
	spawnErr     error
	logger       *slog.Logger
	swiftBinary  string
	spawnTimeout time.Duration
}

// Option configures a Bridge.
type Option func(*Bridge)

// AppInfo describes a running application.
type AppInfo struct {
	Name     string `json:"name"`
	BundleID string `json:"bundle_id,omitempty"`
	PID      int    `json:"pid,omitempty"`
}

// New creates a new macOS bridge.
func New(opts ...Option) *Bridge {
	bridge := &Bridge{
		mode:  "auto",
		shell: &shellClient{},
		caps: map[Capability]bool{
			CapScreenshot:  true,
			CapClick:       true,
			CapType:        true,
			CapAXTree:      true,
			CapScreenDiff:  true,
			CapActionBatch: true,
			CapClipboard:   true,
			CapAppList:     true,
			CapAppFocus:    true,
			CapAppLaunch:   true,
		},
		logger:       slog.Default(),
		spawnTimeout: 5 * time.Second,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(bridge)
		}
	}

	return bridge
}

// WithMode sets the bridge mode.
func WithMode(mode string) Option {
	return func(b *Bridge) {
		b.mode = mode
	}
}

// WithSwiftPath sets the preferred Swift bridge binary path.
func WithSwiftPath(path string) Option {
	return func(b *Bridge) {
		b.swiftBinary = path
	}
}

// WithLogger sets the bridge logger.
func WithLogger(l *slog.Logger) Option {
	return func(b *Bridge) {
		b.logger = l
	}
}

// WithSpawnTimeout sets the Swift bridge spawn timeout.
func WithSpawnTimeout(d time.Duration) Option {
	return func(b *Bridge) {
		b.spawnTimeout = d
	}
}

// IsAvailable checks if we're on macOS.
func (b *Bridge) IsAvailable() bool {
	return runtime.GOOS == "darwin"
}

// Screenshot captures the screen to a temp file, returns the path.
func (b *Bridge) Screenshot(ctx context.Context) (string, error) {
	result, fallback, err := b.trySwift(ctx, CapScreenshot, "screenshot", nil)
	if err != nil {
		return "", err
	}
	if !fallback {
		return decodeScreenshotPath(result)
	}

	return b.shell.Screenshot(ctx)
}

// ScreenshotRegion captures a region of the screen.
func (b *Bridge) ScreenshotRegion(ctx context.Context, x, y, w, h int) (string, error) {
	params := map[string]any{
		"region": map[string]int{
			"x": x,
			"y": y,
			"w": w,
			"h": h,
		},
	}

	result, fallback, err := b.trySwift(ctx, CapScreenshot, "screenshot", params)
	if err != nil {
		return "", err
	}
	if !fallback {
		return decodeScreenshotPath(result)
	}

	return b.shell.ScreenshotRegion(ctx, x, y, w, h)
}

// Click simulates a mouse click at x, y coordinates via AppleScript.
func (b *Bridge) Click(ctx context.Context, x, y int) error {
	params := map[string]int{"x": x, "y": y}

	_, fallback, err := b.trySwift(ctx, CapClick, "click", params)
	if err != nil {
		return err
	}
	if !fallback {
		return nil
	}

	return b.shell.Click(ctx, x, y)
}

// DoubleClick simulates a double click at x, y coordinates.
func (b *Bridge) DoubleClick(ctx context.Context, x, y int) error {
	params := map[string]int{"x": x, "y": y}

	_, fallback, err := b.trySwift(ctx, CapClick, "double_click", params)
	if err != nil {
		return err
	}
	if !fallback {
		return nil
	}

	return b.shell.DoubleClick(ctx, x, y)
}

// RightClick simulates a right click at x, y coordinates.
func (b *Bridge) RightClick(ctx context.Context, x, y int) error {
	params := map[string]int{"x": x, "y": y}

	_, fallback, err := b.trySwift(ctx, CapClick, "right_click", params)
	if err != nil {
		return err
	}
	if !fallback {
		return nil
	}

	return b.shell.RightClick(ctx, x, y)
}

// Type types text at the current cursor position via AppleScript keystroke.
func (b *Bridge) Type(ctx context.Context, text string) error {
	params := map[string]string{"text": text}

	_, fallback, err := b.trySwift(ctx, CapType, "type_text", params)
	if err != nil {
		return err
	}
	if !fallback {
		return nil
	}

	return b.shell.Type(ctx, text)
}

// Key sends a keyboard shortcut like "command+v", "ctrl+c", "return".
func (b *Bridge) Key(ctx context.Context, keys string) error {
	params := map[string]string{"keys": keys}

	_, fallback, err := b.trySwift(ctx, CapType, "key_combo", params)
	if err != nil {
		return err
	}
	if !fallback {
		return nil
	}

	return b.shell.Key(ctx, keys)
}

// ClipboardRead reads text from the system clipboard.
func (b *Bridge) ClipboardRead(ctx context.Context) (string, error) {
	result, fallback, err := b.trySwift(ctx, CapClipboard, "clipboard_read", nil)
	if err != nil {
		return "", err
	}
	if !fallback {
		return decodeStringResult(result)
	}

	return b.shell.ClipboardRead(ctx)
}

// ClipboardWrite writes text to the system clipboard.
func (b *Bridge) ClipboardWrite(ctx context.Context, text string) error {
	params := map[string]string{"text": text}

	_, fallback, err := b.trySwift(ctx, CapClipboard, "clipboard_write", params)
	if err != nil {
		return err
	}
	if !fallback {
		return nil
	}

	return b.shell.ClipboardWrite(ctx, text)
}

// ListApps returns running foreground applications.
func (b *Bridge) ListApps(ctx context.Context) ([]AppInfo, error) {
	result, fallback, err := b.trySwift(ctx, CapAppList, "list_apps", nil)
	if err != nil {
		return nil, err
	}
	if !fallback {
		return decodeAppsResult(result)
	}

	return b.shell.ListApps(ctx)
}

// FocusApp brings an application to the foreground.
func (b *Bridge) FocusApp(ctx context.Context, appName string) error {
	params := map[string]string{"app_name": appName}

	_, fallback, err := b.trySwift(ctx, CapAppFocus, "focus_app", params)
	if err != nil {
		return err
	}
	if !fallback {
		return nil
	}

	return b.shell.FocusApp(ctx, appName)
}

// LaunchApp opens an application by name.
func (b *Bridge) LaunchApp(ctx context.Context, appName string) error {
	params := map[string]string{"app_name": appName}

	_, fallback, err := b.trySwift(ctx, CapAppLaunch, "launch_app", params)
	if err != nil {
		return err
	}
	if !fallback {
		return nil
	}

	return b.shell.LaunchApp(ctx, appName)
}

// Close shuts down the Swift bridge process if it was started.
func (b *Bridge) Close() error {
	b.mu.Lock()
	swift := b.swift
	b.mu.Unlock()

	if swift != nil {
		return swift.Close(context.Background())
	}

	return nil
}

// --- Internal Helpers ---

func (b *Bridge) trySwift(
	ctx context.Context,
	cap Capability,
	method string,
	params any,
) (json.RawMessage, bool, error) {
	if !b.useSwift(cap) {
		return nil, true, nil
	}

	b.mu.Lock()
	swift := b.swift
	b.mu.Unlock()

	if swift == nil {
		return nil, true, nil
	}

	result, err := swift.call(ctx, method, params)
	if err == nil {
		return result, false, nil
	}

	var protocolErr *ProtocolError
	if errors.As(err, &protocolErr) && isTransient(protocolErr.Code) {
		b.disableCapability(cap)
		return nil, true, nil
	}

	return nil, false, err
}

func (b *Bridge) useSwift(cap Capability) bool {
	if b.mode == "shell" {
		return false
	}

	b.mu.Lock()
	enabled := b.caps[cap]
	swift := b.swift
	spawnErr := b.spawnErr
	swiftBinary := b.swiftBinary
	spawnTimeout := b.spawnTimeout
	logger := b.logger
	b.mu.Unlock()

	if !enabled {
		return false
	}
	if swift != nil && spawnErr == nil {
		return true
	}

	b.spawnOnce.Do(func() {
		binary := lookupSwiftBinary(swiftBinary)
		if binary == "" {
			err := fmt.Errorf("swift bridge binary not found")
			b.mu.Lock()
			b.spawnErr = err
			b.mu.Unlock()
			if logger != nil {
				logger.Warn("swift bridge unavailable, falling back to shell", "error", err)
			}
			return
		}

		client, err := spawnSwift(context.Background(), binary, spawnTimeout)

		b.mu.Lock()
		b.swift = client
		b.spawnErr = err
		b.mu.Unlock()

		if err != nil && logger != nil {
			logger.Warn("swift bridge unavailable, falling back to shell", "path", binary, "error", err)
		}
	})

	b.mu.Lock()
	defer b.mu.Unlock()

	return b.caps[cap] && b.swift != nil && b.spawnErr == nil
}

func (b *Bridge) disableCapability(cap Capability) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.caps[cap] = false
}

func decodeScreenshotPath(result json.RawMessage) (string, error) {
	path, err := decodeStringResult(result)
	if err == nil {
		return path, nil
	}

	var payload struct {
		Path string `json:"path"`
	}
	if unmarshalErr := json.Unmarshal(result, &payload); unmarshalErr == nil && payload.Path != "" {
		return payload.Path, nil
	}

	return "", fmt.Errorf("invalid screenshot result: %w", err)
}

func decodeStringResult(result json.RawMessage) (string, error) {
	var value string
	if err := json.Unmarshal(result, &value); err != nil {
		return "", fmt.Errorf("failed to decode string result: %w", err)
	}

	return value, nil
}

func decodeAppsResult(result json.RawMessage) ([]AppInfo, error) {
	var apps []AppInfo
	if err := json.Unmarshal(result, &apps); err != nil {
		return nil, fmt.Errorf("failed to decode app list: %w", err)
	}

	return apps, nil
}

func isTransient(code string) bool {
	switch code {
	case ErrPermissionDenied, ErrTimeout, ErrUnsupportedOS:
		return true
	default:
		return false
	}
}

func lookupSwiftBinary(cfgPath string) string {
	candidates := make([]string, 0, 5)
	if cfgPath != "" {
		candidates = append(candidates, cfgPath)
	}

	if xdgDataHome := os.Getenv("XDG_DATA_HOME"); xdgDataHome != "" {
		candidates = append(candidates, filepath.Join(xdgDataHome, "providence", "providence-mac-bridge"))
	}

	if executable, err := osExecutableFunc(); err == nil && executable != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "providence-mac-bridge"))
	}

	candidates = append(
		candidates,
		os.ExpandEnv("$HOME/.providence/bin/providence-mac-bridge"),
		swiftGlobalInstallDir,
	)

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return ""
}
