package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gravitrone/providence-core/internal/bridge/macos"
)

// DiffBridge is the subset of *macos.Bridge that ScreenDiff needs.
type DiffBridge interface {
	ScreenDiff(ctx context.Context, p macos.ScreenDiffParams) (macos.ScreenDiffResult, error)
}

// ScreenDiffTool checks what changed on screen since the last captured frame.
type ScreenDiffTool struct {
	bridge DiffBridge
}

// NewScreenDiffTool creates a new ScreenDiff tool.
func NewScreenDiffTool(b DiffBridge) *ScreenDiffTool {
	return &ScreenDiffTool{bridge: b}
}

// Name implements Tool.
func (t *ScreenDiffTool) Name() string { return "ScreenDiff" }

// Description implements Tool.
func (t *ScreenDiffTool) Description() string {
	return "Compare the current screen against the last captured frame using a perceptual hash. Returns changed regions with coordinates and magnitude."
}

// ReadOnly implements Tool.
func (t *ScreenDiffTool) ReadOnly() bool { return true }

// InputSchema implements Tool.
func (t *ScreenDiffTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"since_ts_ns":   map[string]any{"type": "integer", "description": "Timestamp in ns; 0 = diff against last captured frame"},
			"max_regions":   map[string]any{"type": "integer", "description": "Default 8."},
			"min_magnitude": map[string]any{"type": "number", "description": "Minimum change ratio per region (0..1). Default 0.02."},
		},
	}
}

// Execute implements Tool.
func (t *ScreenDiffTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	raw, err := json.Marshal(input)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to encode params: %v", err), IsError: true}
	}

	var p macos.ScreenDiffParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}
	}

	result, err := t.bridge.ScreenDiff(ctx, p)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("screen_diff failed: %v", err), IsError: true}
	}

	out, err := json.Marshal(result)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to encode result: %v", err), IsError: true}
	}

	return ToolResult{Content: string(out)}
}

// Prompt implements ToolPrompter.
func (t *ScreenDiffTool) Prompt() string {
	return `Use ScreenDiff to check what changed on screen since the last screenshot. Returns a perceptual hash comparison + list of changed regions with coordinates and magnitude.

Prefer over a full Screenshot when you just need to detect change - diff returns ~50 bytes instead of a 1MB PNG. When regions are returned, call Screenshot with {region: {...}} for targeted visual confirmation of just the changed area.

This pairs with DesktopActionBatch verify_ax mode: after a batch, use ScreenDiff to confirm the expected visual state changed.`
}
