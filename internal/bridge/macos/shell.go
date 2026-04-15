//go:build darwin

package macos

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type shellClient struct{}

// Screenshot captures the screen to a temp file, returns the path.
func (c *shellClient) Screenshot(ctx context.Context) (string, error) {
	if runtime.GOOS != "darwin" {
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
func (c *shellClient) ScreenshotRegion(ctx context.Context, x, y, w, h int) (string, error) {
	if runtime.GOOS != "darwin" {
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
func (c *shellClient) Click(ctx context.Context, x, y int) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("computer use only available on macOS")
	}
	script := fmt.Sprintf(`tell application "System Events" to click at {%d, %d}`, x, y)
	return c.runOsascript(ctx, script)
}

// DoubleClick simulates a double click at x, y coordinates.
func (c *shellClient) DoubleClick(ctx context.Context, x, y int) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("computer use only available on macOS")
	}
	script := fmt.Sprintf(`tell application "System Events" to double click at {%d, %d}`, x, y)
	return c.runOsascript(ctx, script)
}

// RightClick simulates a right click at x, y coordinates.
func (c *shellClient) RightClick(ctx context.Context, x, y int) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("computer use only available on macOS")
	}
	// AppleScript doesn't have a direct right-click, use click with control key
	script := fmt.Sprintf(`tell application "System Events"
	key down control
	click at {%d, %d}
	key up control
end tell`, x, y)
	return c.runOsascript(ctx, script)
}

// Type types text at the current cursor position via AppleScript keystroke.
func (c *shellClient) Type(ctx context.Context, text string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("computer use only available on macOS")
	}
	escaped := strings.ReplaceAll(text, `"`, `\"`)
	escaped = strings.ReplaceAll(escaped, `\`, `\\`)
	script := fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, escaped)
	return c.runOsascript(ctx, script)
}

// Key sends a keyboard shortcut like "command+v", "ctrl+c", "return".
func (c *shellClient) Key(ctx context.Context, keys string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("computer use only available on macOS")
	}
	script, err := buildKeystrokeScript(keys)
	if err != nil {
		return err
	}
	return c.runOsascript(ctx, script)
}

// ClipboardRead reads text from the system clipboard.
func (c *shellClient) ClipboardRead(ctx context.Context) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("computer use only available on macOS")
	}
	out, err := exec.CommandContext(ctx, "pbpaste").Output()
	if err != nil {
		return "", fmt.Errorf("clipboard read failed: %w", err)
	}
	return string(out), nil
}

// ClipboardWrite writes text to the system clipboard.
func (c *shellClient) ClipboardWrite(ctx context.Context, text string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("computer use only available on macOS")
	}
	cmd := exec.CommandContext(ctx, "pbcopy")
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clipboard write failed: %w", err)
	}
	return nil
}

// ListApps returns running foreground applications.
func (c *shellClient) ListApps(ctx context.Context) ([]AppInfo, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("computer use only available on macOS")
	}
	script := `tell application "System Events" to get name of every process whose background only is false`
	out, err := c.runOsascriptOutput(ctx, script)
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
func (c *shellClient) FocusApp(ctx context.Context, appName string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("computer use only available on macOS")
	}
	escaped := strings.ReplaceAll(appName, `"`, `\"`)
	script := fmt.Sprintf(`tell application "%s" to activate`, escaped)
	return c.runOsascript(ctx, script)
}

// LaunchApp opens an application by name.
func (c *shellClient) LaunchApp(ctx context.Context, appName string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("computer use only available on macOS")
	}
	return exec.CommandContext(ctx, "open", "-a", appName).Run()
}

// osascriptTimeout caps per-call osascript execution. Prevents hangs when a
// TCC Automation prompt is pending and the GUI can't display it (CI, headless
// shells, test runs). 15s is generous for normal responses.
const osascriptTimeout = 15 * time.Second

func (c *shellClient) runOsascript(ctx context.Context, script string) error {
	ctx, cancel := context.WithTimeout(ctx, osascriptTimeout)
	defer cancel()
	_, err := runOsascriptIsolated(ctx, script, true)
	return err
}

func (c *shellClient) runOsascriptOutput(ctx context.Context, script string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, osascriptTimeout)
	defer cancel()
	return runOsascriptIsolated(ctx, script, false)
}

// runOsascriptIsolated runs osascript in its own Unix process group so a
// ctx timeout can kill both the shell parent and any `do shell script`
// children in one syscall. Without this, `exec.CommandContext` only
// SIGKILLs osascript itself, while children (e.g. a `sleep` inside a
// `do shell script` block) survive, keep the stdout/stderr pipe open, and
// block the caller indefinitely.
//
// combined=true returns stdout+stderr concatenated as the "output" buffer
// for error reporting (mirrors the prior cmd.CombinedOutput() contract
// that runOsascript relied on). combined=false returns only stdout.
func runOsascriptIsolated(ctx context.Context, script string, combined bool) (string, error) {
	cmd := exec.Command("osascript", "-e", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	if combined {
		cmd.Stderr = &stdout
	} else {
		cmd.Stderr = &stderr
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("osascript start: %w", err)
	}

	// Kill the whole process group when ctx fires. The watcher exits when
	// the command finishes naturally (done channel closes).
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		case <-done:
		}
	}()

	waitErr := cmd.Wait()
	close(done)

	// If ctx tripped the watcher, surface the ctx error so callers can
	// distinguish "osascript returned non-zero" from "we killed it".
	if ctxErr := ctx.Err(); ctxErr != nil {
		return stdout.String(), fmt.Errorf("osascript cancelled: %w", ctxErr)
	}
	if waitErr != nil {
		return stdout.String(), fmt.Errorf("osascript failed: %w: %s", waitErr, stdout.String())
	}
	return stdout.String(), nil
}

var appleScriptModifierMap = map[string]string{
	"cmd":     "command down",
	"control": "control down",
	"option":  "option down",
	"shift":   "shift down",
}

// buildKeystrokeScript parses a key combo like "command+v" into AppleScript.
func buildKeystrokeScript(keys string) (string, error) {
	combo, err := ParseKeyCombo(keys)
	if err != nil {
		return "", err
	}

	modifiers := make([]string, 0, len(combo.Modifiers))
	for _, modifier := range combo.Modifiers {
		modifiers = append(modifiers, appleScriptModifierMap[modifier])
	}

	if usesAppleScriptKeyCode(combo.Key) {
		if len(modifiers) > 0 {
			return fmt.Sprintf(`tell application "System Events" to key code %d using {%s}`,
				combo.VirtualCode, strings.Join(modifiers, ", ")), nil
		}
		return fmt.Sprintf(`tell application "System Events" to key code %d`, combo.VirtualCode), nil
	}

	if len(modifiers) > 0 {
		return fmt.Sprintf(`tell application "System Events" to keystroke "%s" using {%s}`,
			combo.Key, strings.Join(modifiers, ", ")), nil
	}
	return fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, combo.Key), nil
}
