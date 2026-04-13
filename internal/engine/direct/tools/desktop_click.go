package tools

import (
	"context"
	"fmt"
	"runtime"

	"github.com/gravitrone/providence-core/internal/bridge/macos"
)

// DesktopClickTool clicks at screen coordinates.
type DesktopClickTool struct {
	bridge *macos.Bridge
}

// NewDesktopClickTool creates a new desktop click tool.
func NewDesktopClickTool(bridge *macos.Bridge) *DesktopClickTool {
	return &DesktopClickTool{bridge: bridge}
}

func (t *DesktopClickTool) Name() string { return "DesktopClick" }
func (t *DesktopClickTool) Description() string {
	return "Click at screen coordinates. Supports click, double_click, and right_click actions."
}
func (t *DesktopClickTool) ReadOnly() bool { return false }

// Prompt implements ToolPrompter.
func (t *DesktopClickTool) Prompt() string {
	return `Click at screen coordinates. Supports click, double_click, and right_click actions. Always take a Screenshot first to identify the target position.`
}

func (t *DesktopClickTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"x": map[string]any{
				"type":        "integer",
				"description": "X screen coordinate.",
			},
			"y": map[string]any{
				"type":        "integer",
				"description": "Y screen coordinate.",
			},
			"action": map[string]any{
				"type":        "string",
				"description": "Click action: click, double_click, or right_click.",
				"enum":        []string{"click", "double_click", "right_click"},
				"default":     "click",
			},
		},
		"required": []string{"x", "y"},
	}
}

func (t *DesktopClickTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	if runtime.GOOS != "darwin" {
		return ToolResult{Content: "desktop click only available on macOS", IsError: true}
	}

	x := paramInt(input, "x", -1)
	y := paramInt(input, "y", -1)
	if x < 0 || y < 0 {
		return ToolResult{Content: "x and y coordinates are required and must be non-negative", IsError: true}
	}

	action := paramString(input, "action", "click")

	var err error
	switch action {
	case "click":
		err = t.bridge.Click(ctx, x, y)
	case "double_click":
		err = t.bridge.DoubleClick(ctx, x, y)
	case "right_click":
		err = t.bridge.RightClick(ctx, x, y)
	default:
		return ToolResult{Content: fmt.Sprintf("unknown action: %s", action), IsError: true}
	}

	if err != nil {
		return ToolResult{Content: fmt.Sprintf("click failed: %v", err), IsError: true}
	}

	return ToolResult{Content: fmt.Sprintf("Clicked at (%d, %d) with action %s", x, y, action)}
}
