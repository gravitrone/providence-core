package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gravitrone/providence-core/internal/bridge/macos"
)

// BatchBridge is the subset of *macos.Bridge that DesktopActionBatch needs.
type BatchBridge interface {
	ActionBatch(ctx context.Context, p macos.ActionBatchParams) (macos.ActionBatchResult, error)
}

// DesktopActionBatchTool executes multiple UI actions in a single round trip.
type DesktopActionBatchTool struct {
	bridge BatchBridge
}

// NewDesktopActionBatchTool creates a new DesktopActionBatch tool.
func NewDesktopActionBatchTool(b BatchBridge) *DesktopActionBatchTool {
	return &DesktopActionBatchTool{bridge: b}
}

// Name implements Tool.
func (t *DesktopActionBatchTool) Name() string { return "DesktopActionBatch" }

// Description implements Tool.
func (t *DesktopActionBatchTool) Description() string {
	return "Execute multiple UI actions in a single round trip - click, type, key, wait, verify_ax, read_value, focus_app."
}

// ReadOnly implements Tool.
func (t *DesktopActionBatchTool) ReadOnly() bool { return false }

// InputSchema implements Tool.
func (t *DesktopActionBatchTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"actions": map[string]any{
				"type":        "array",
				"description": "Ordered list of actions to execute server-side in one round trip.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"type":   map[string]any{"type": "string", "enum": []string{"click", "click_element", "type", "key", "wait", "verify_ax", "read_value", "focus_app"}},
						"params": map[string]any{"type": "object", "description": "Action-specific params. See tool guidance for schemas."},
					},
					"required": []string{"type"},
				},
			},
			"stop_on_error":    map[string]any{"type": "boolean", "description": "Default true. If false, continues past failures."},
			"screenshot_after": map[string]any{"type": "boolean", "description": "Capture + return a screenshot path after the batch completes."},
		},
		"required": []string{"actions"},
	}
}

// Execute implements Tool.
func (t *DesktopActionBatchTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	raw, err := json.Marshal(input)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to encode params: %v", err), IsError: true}
	}

	var p macos.ActionBatchParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}
	}

	if len(p.Actions) == 0 {
		return ToolResult{Content: "actions list is empty", IsError: true}
	}

	result, err := t.bridge.ActionBatch(ctx, p)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("action_batch failed: %v", err), IsError: true}
	}

	out, err := json.Marshal(result)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to encode result: %v", err), IsError: true}
	}

	return ToolResult{Content: string(out)}
}

// Prompt implements ToolPrompter.
func (t *DesktopActionBatchTool) Prompt() string {
	return `Use DesktopActionBatch for multi-step UI automation in a SINGLE round trip. Instead of N screenshots + N clicks + N model calls, batch click+type+verify into one call.

Action types and their params:
- "click": {"x": int, "y": int, "button"?: "left|right|middle", "count"?: int}
- "click_element": {"query": {...AXQuery}, "action"?: "click|double_click|right_click"}
- "type": {"text": string, "delay_ms"?: int}
- "key": {"combo": "cmd+shift+z"}
- "wait": {"ms": int}
- "verify_ax": {"expect": {...AXQuery matching any element}}  // fails the batch if no match
- "read_value": {"element_id": string} -> reads AX value and returns it in result
- "focus_app": {"app": string}

Rules:
- Actions run sequentially. If stop_on_error=true (default) and an action fails, subsequent actions are skipped.
- verify_ax uses the AX tree (<5ms) instead of re-screenshotting, so verification is nearly free.
- Focus changes (cmd+tab, click into another app) mid-batch abort with error.code=focus_changed.

Prefer ActionBatch over single Click/Type/Key calls whenever you're doing a known flow (search, form fill, menu navigation).`
}
