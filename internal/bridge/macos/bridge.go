package macos

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Bridge provides macOS computer use capabilities via shell fallbacks.
// Future versions will use a Swift subprocess for AX tree, region capture, etc.
type Bridge struct {
	// swiftBridge *exec.Cmd - reserved for future Swift subprocess
}

// New creates a new macOS bridge.
func New() *Bridge {
	return &Bridge{}
}

// IsAvailable checks if we're on macOS.
func (b *Bridge) IsAvailable() bool {
	return runtime.GOOS == "darwin"
}

// Screenshot captures the screen to a temp file, returns the path.
func (b *Bridge) Screenshot(ctx context.Context) (string, error) {
	if !b.IsAvailable() {
		return "", fmt.Errorf("computer use only available on macOS")
	}
	tmpFile := fmt.Sprintf("/tmp/providence-screenshot-%d.png", time.Now().UnixMilli())
	cmd := exec.CommandContext(ctx, "screencapture", "-x", "-t", "png", tmpFile)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("screenshot failed: %w", err)
	}
	return tmpFile, nil
}

// ScreenshotRegion captures a region of the screen.
func (b *Bridge) ScreenshotRegion(ctx context.Context, x, y, w, h int) (string, error) {
	if !b.IsAvailable() {
		return "", fmt.Errorf("computer use only available on macOS")
	}
	tmpFile := fmt.Sprintf("/tmp/providence-screenshot-%d.png", time.Now().UnixMilli())
	region := fmt.Sprintf("-R%d,%d,%d,%d", x, y, w, h)
	cmd := exec.CommandContext(ctx, "screencapture", "-x", "-t", "png", region, tmpFile)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("screenshot region failed: %w", err)
	}
	return tmpFile, nil
}

// Click simulates a mouse click at x, y coordinates via AppleScript.
func (b *Bridge) Click(ctx context.Context, x, y int) error {
	if !b.IsAvailable() {
		return fmt.Errorf("computer use only available on macOS")
	}
	script := fmt.Sprintf(`tell application "System Events" to click at {%d, %d}`, x, y)
	return b.runOsascript(ctx, script)
}

// DoubleClick simulates a double click at x, y coordinates.
func (b *Bridge) DoubleClick(ctx context.Context, x, y int) error {
	if !b.IsAvailable() {
		return fmt.Errorf("computer use only available on macOS")
	}
	script := fmt.Sprintf(`tell application "System Events" to double click at {%d, %d}`, x, y)
	return b.runOsascript(ctx, script)
}

// RightClick simulates a right click at x, y coordinates.
func (b *Bridge) RightClick(ctx context.Context, x, y int) error {
	if !b.IsAvailable() {
		return fmt.Errorf("computer use only available on macOS")
	}
	// AppleScript doesn't have a direct right-click, use click with control key
	script := fmt.Sprintf(`tell application "System Events"
	key down control
	click at {%d, %d}
	key up control
end tell`, x, y)
	return b.runOsascript(ctx, script)
}

// Type types text at the current cursor position via AppleScript keystroke.
func (b *Bridge) Type(ctx context.Context, text string) error {
	if !b.IsAvailable() {
		return fmt.Errorf("computer use only available on macOS")
	}
	escaped := strings.ReplaceAll(text, `"`, `\"`)
	escaped = strings.ReplaceAll(escaped, `\`, `\\`)
	script := fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, escaped)
	return b.runOsascript(ctx, script)
}

// Key sends a keyboard shortcut like "command+v", "ctrl+c", "return".
func (b *Bridge) Key(ctx context.Context, keys string) error {
	if !b.IsAvailable() {
		return fmt.Errorf("computer use only available on macOS")
	}
	script, err := buildKeystrokeScript(keys)
	if err != nil {
		return err
	}
	return b.runOsascript(ctx, script)
}

// ClipboardRead reads text from the system clipboard.
func (b *Bridge) ClipboardRead(ctx context.Context) (string, error) {
	if !b.IsAvailable() {
		return "", fmt.Errorf("computer use only available on macOS")
	}
	out, err := exec.CommandContext(ctx, "pbpaste").Output()
	if err != nil {
		return "", fmt.Errorf("clipboard read failed: %w", err)
	}
	return string(out), nil
}

// ClipboardWrite writes text to the system clipboard.
func (b *Bridge) ClipboardWrite(ctx context.Context, text string) error {
	if !b.IsAvailable() {
		return fmt.Errorf("computer use only available on macOS")
	}
	cmd := exec.CommandContext(ctx, "pbcopy")
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clipboard write failed: %w", err)
	}
	return nil
}

// AppInfo describes a running application.
type AppInfo struct {
	Name     string `json:"name"`
	BundleID string `json:"bundle_id,omitempty"`
	PID      int    `json:"pid,omitempty"`
}

// ListApps returns running foreground applications.
func (b *Bridge) ListApps(ctx context.Context) ([]AppInfo, error) {
	if !b.IsAvailable() {
		return nil, fmt.Errorf("computer use only available on macOS")
	}
	script := `tell application "System Events" to get name of every process whose background only is false`
	out, err := b.runOsascriptOutput(ctx, script)
	if err != nil {
		return nil, fmt.Errorf("list apps failed: %w", err)
	}
	names := strings.Split(strings.TrimSpace(out), ", ")
	apps := make([]AppInfo, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			apps = append(apps, AppInfo{Name: name})
		}
	}
	return apps, nil
}

// FocusApp brings an application to the foreground.
func (b *Bridge) FocusApp(ctx context.Context, appName string) error {
	if !b.IsAvailable() {
		return fmt.Errorf("computer use only available on macOS")
	}
	escaped := strings.ReplaceAll(appName, `"`, `\"`)
	script := fmt.Sprintf(`tell application "%s" to activate`, escaped)
	return b.runOsascript(ctx, script)
}

// LaunchApp opens an application by name.
func (b *Bridge) LaunchApp(ctx context.Context, appName string) error {
	if !b.IsAvailable() {
		return fmt.Errorf("computer use only available on macOS")
	}
	return exec.CommandContext(ctx, "open", "-a", appName).Run()
}

func (b *Bridge) runOsascript(ctx context.Context, script string) error {
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("osascript failed: %w: %s", err, string(out))
	}
	return nil
}

func (b *Bridge) runOsascriptOutput(ctx context.Context, script string) (string, error) {
	out, err := exec.CommandContext(ctx, "osascript", "-e", script).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// keyMap maps common key names to AppleScript key code references.
var keyMap = map[string]int{
	"return":    36,
	"enter":     36,
	"tab":       48,
	"space":     49,
	"delete":    51,
	"backspace": 51,
	"escape":    53,
	"esc":       53,
	"up":        126,
	"down":      125,
	"left":      123,
	"right":     124,
	"home":      115,
	"end":       119,
	"pageup":    116,
	"pagedown":  121,
	"f1":        122,
	"f2":        120,
	"f3":        99,
	"f4":        118,
	"f5":        96,
	"f6":        97,
	"f7":        98,
	"f8":        100,
	"f9":        101,
	"f10":       109,
	"f11":       103,
	"f12":       111,
}

// modifierMap maps modifier names to AppleScript modifier syntax.
var modifierMap = map[string]string{
	"command": "command down",
	"cmd":     "command down",
	"control": "control down",
	"ctrl":    "control down",
	"option":  "option down",
	"alt":     "option down",
	"shift":   "shift down",
}

// buildKeystrokeScript parses a key combo like "command+v" into AppleScript.
func buildKeystrokeScript(keys string) (string, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(keys)), "+")
	if len(parts) == 0 {
		return "", fmt.Errorf("empty key combo")
	}

	var modifiers []string
	var keyPart string

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if mod, ok := modifierMap[p]; ok {
			modifiers = append(modifiers, mod)
		} else {
			keyPart = p
		}
	}

	if keyPart == "" {
		return "", fmt.Errorf("no key specified in combo: %s", keys)
	}

	// Check if it's a special key (needs key code) or a character (needs keystroke)
	if code, ok := keyMap[keyPart]; ok {
		// Special key - use key code
		if len(modifiers) > 0 {
			return fmt.Sprintf(`tell application "System Events" to key code %d using {%s}`,
				code, strings.Join(modifiers, ", ")), nil
		}
		return fmt.Sprintf(`tell application "System Events" to key code %d`, code), nil
	}

	// Regular character - use keystroke
	if len(modifiers) > 0 {
		return fmt.Sprintf(`tell application "System Events" to keystroke "%s" using {%s}`,
			keyPart, strings.Join(modifiers, ", ")), nil
	}
	return fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, keyPart), nil
}
