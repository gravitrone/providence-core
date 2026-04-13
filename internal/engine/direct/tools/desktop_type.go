package tools

import (
	"context"
	"fmt"
	"runtime"

	"github.com/gravitrone/providence-core/internal/bridge/macos"
)

// DesktopTypeTool types text or sends keyboard shortcuts.
type DesktopTypeTool struct {
	bridge *macos.Bridge
}

// NewDesktopTypeTool creates a new desktop type tool.
func NewDesktopTypeTool(bridge *macos.Bridge) *DesktopTypeTool {
	return &DesktopTypeTool{bridge: bridge}
}

func (t *DesktopTypeTool) Name() string { return "DesktopType" }
func (t *DesktopTypeTool) Description() string {
	return "Type text at the current cursor position or send a keyboard shortcut (e.g. command+v)."
}
func (t *DesktopTypeTool) ReadOnly() bool { return false }

// Prompt implements ToolPrompter.
func (t *DesktopTypeTool) Prompt() string {
	return `Type text at the current cursor position or send a keyboard shortcut (e.g. command+v). Use after DesktopClick to interact with GUI applications.`
}

func (t *DesktopTypeTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{
				"type":        "string",
				"description": "Text to type at the cursor position.",
			},
			"key": map[string]any{
				"type":        "string",
				"description": "Keyboard shortcut to send (e.g. command+v, ctrl+c, return).",
			},
		},
	}
}

func (t *DesktopTypeTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	if runtime.GOOS != "darwin" {
		return ToolResult{Content: "desktop type only available on macOS", IsError: true}
	}

	text := paramString(input, "text", "")
	key := paramString(input, "key", "")

	if text == "" && key == "" {
		return ToolResult{Content: "either text or key is required", IsError: true}
	}

	// If both provided, type text first then send key
	if text != "" {
		if err := t.bridge.Type(ctx, text); err != nil {
			return ToolResult{Content: fmt.Sprintf("type failed: %v", err), IsError: true}
		}
	}

	if key != "" {
		if err := t.bridge.Key(ctx, key); err != nil {
			return ToolResult{Content: fmt.Sprintf("key press failed: %v", err), IsError: true}
		}
		if text != "" {
			return ToolResult{Content: fmt.Sprintf("Typed: %s, then pressed: %s", text, key)}
		}
		return ToolResult{Content: fmt.Sprintf("Pressed: %s", key)}
	}

	return ToolResult{Content: fmt.Sprintf("Typed: %s", text)}
}
