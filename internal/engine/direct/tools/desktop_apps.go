package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/gravitrone/providence-core/internal/bridge/macos"
)

// DesktopAppsTool manages applications - list, focus, or launch.
type DesktopAppsTool struct {
	bridge *macos.Bridge
}

// NewDesktopAppsTool creates a new desktop apps tool.
func NewDesktopAppsTool(bridge *macos.Bridge) *DesktopAppsTool {
	return &DesktopAppsTool{bridge: bridge}
}

func (t *DesktopAppsTool) Name() string { return "DesktopApps" }
func (t *DesktopAppsTool) Description() string {
	return "List running applications, focus an app, or launch an app by name."
}
func (t *DesktopAppsTool) ReadOnly() bool { return true }

// Prompt implements ToolPrompter.
func (t *DesktopAppsTool) Prompt() string {
	return `List running applications, focus an app by name, or launch a new app. macOS only. Use for computer-use workflows that need to switch between apps.`
}

func (t *DesktopAppsTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action to perform: list, focus, or launch.",
				"enum":        []string{"list", "focus", "launch"},
			},
			"app": map[string]any{
				"type":        "string",
				"description": "Application name (required for focus and launch).",
			},
		},
		"required": []string{"action"},
	}
}

func (t *DesktopAppsTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	if runtime.GOOS != "darwin" {
		return ToolResult{Content: "desktop apps only available on macOS", IsError: true}
	}

	action := paramString(input, "action", "")
	app := paramString(input, "app", "")

	switch action {
	case "list":
		apps, err := t.bridge.ListApps(ctx)
		if err != nil {
			return ToolResult{Content: fmt.Sprintf("list apps failed: %v", err), IsError: true}
		}
		data, _ := json.Marshal(apps)
		return ToolResult{Content: string(data)}

	case "focus":
		if app == "" {
			return ToolResult{Content: "app name is required for focus action", IsError: true}
		}
		if err := t.bridge.FocusApp(ctx, app); err != nil {
			return ToolResult{Content: fmt.Sprintf("focus app failed: %v", err), IsError: true}
		}
		return ToolResult{Content: fmt.Sprintf("Focused: %s", app)}

	case "launch":
		if app == "" {
			return ToolResult{Content: "app name is required for launch action", IsError: true}
		}
		if err := t.bridge.LaunchApp(ctx, app); err != nil {
			return ToolResult{Content: fmt.Sprintf("launch app failed: %v", err), IsError: true}
		}
		return ToolResult{Content: fmt.Sprintf("Launched: %s", app)}

	default:
		return ToolResult{Content: fmt.Sprintf("unknown action: %s (use list, focus, or launch)", action), IsError: true}
	}
}
