package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/gravitrone/providence-core/internal/bridge/macos"
)

// DesktopReadScreenTool returns the Accessibility tree for a target app.
type DesktopReadScreenTool struct {
	bridge AXBridge
}

// NewDesktopReadScreenTool creates a new DesktopReadScreen tool.
func NewDesktopReadScreenTool(b AXBridge) *DesktopReadScreenTool {
	return &DesktopReadScreenTool{bridge: b}
}

func (t *DesktopReadScreenTool) Name() string { return "DesktopReadScreen" }
func (t *DesktopReadScreenTool) Description() string {
	return "Read the Accessibility tree of a macOS app as structured or flat text."
}
func (t *DesktopReadScreenTool) ReadOnly() bool { return true }

// Prompt implements ToolPrompter.
func (t *DesktopReadScreenTool) Prompt() string {
	return `Use DesktopReadScreen to get a full semantic snapshot of a macOS app's UI without taking a screenshot. Returns either a flat text outline (default) or a JSON tree. Prefer this when you need to understand app layout, find elements, or verify state - it's faster and more token-efficient than Screenshot.`
}

// InputSchema returns the JSON Schema for DesktopReadScreen.
func (t *DesktopReadScreenTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"app":               map[string]any{"type": "string", "description": "Bundle ID or app name. Omit for frontmost."},
			"max_depth":         map[string]any{"type": "integer", "description": "Max tree depth. Default 12."},
			"max_nodes":         map[string]any{"type": "integer", "description": "Max nodes to return. Default 2000."},
			"include_invisible": map[string]any{"type": "boolean", "description": "Include hidden elements. Default false."},
			"format":            map[string]any{"type": "string", "enum": []string{"json", "flat"}, "description": "Output format. Default flat."},
		},
	}
}

// Execute implements Tool.
func (t *DesktopReadScreenTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	if runtime.GOOS != "darwin" {
		return ToolResult{Content: "DesktopReadScreen only available on macOS", IsError: true}
	}

	p := macos.AXTreeParams{
		App:              paramString(input, "app", ""),
		MaxDepth:         paramInt(input, "max_depth", 12),
		MaxNodes:         paramInt(input, "max_nodes", 2000),
		IncludeInvisible: paramBool(input, "include_invisible", false),
		Format:           paramString(input, "format", "flat"),
	}

	result, err := t.bridge.AXTree(ctx, p)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("ax_tree failed: %v", err), IsError: true}
	}

	if p.Format == "json" {
		out, err := json.Marshal(result.Root)
		if err != nil {
			return ToolResult{Content: fmt.Sprintf("failed to encode tree: %v", err), IsError: true}
		}
		return ToolResult{Content: string(out)}
	}

	return ToolResult{Content: result.Flat}
}
