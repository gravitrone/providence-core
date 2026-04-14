package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/gravitrone/providence-core/internal/bridge/macos"
)

// AXBridge is the subset of *macos.Bridge that the AX tools need, extracted for mockability.
type AXBridge interface {
	AXFind(ctx context.Context, q macos.AXQuery) (macos.AXFindResult, error)
	AXTree(ctx context.Context, p macos.AXTreeParams) (macos.AXTreeResult, error)
	AXPerform(ctx context.Context, elementID, action string) error
	Click(ctx context.Context, x, y int) error
	DoubleClick(ctx context.Context, x, y int) error
}

// DesktopFindElementTool locates UI elements by semantic attributes.
type DesktopFindElementTool struct {
	bridge AXBridge
}

// NewDesktopFindElementTool creates a new DesktopFindElement tool.
func NewDesktopFindElementTool(b AXBridge) *DesktopFindElementTool {
	return &DesktopFindElementTool{bridge: b}
}

func (t *DesktopFindElementTool) Name() string { return "DesktopFindElement" }
func (t *DesktopFindElementTool) Description() string {
	return "Find UI elements by role, title, or text using the macOS Accessibility tree."
}
func (t *DesktopFindElementTool) ReadOnly() bool { return true }

// Prompt implements ToolPrompter.
func (t *DesktopFindElementTool) Prompt() string {
	return `Use DesktopFindElement to locate UI elements by semantics (role, title, text) instead of pixel coordinates. Returns an array of matches with stable element IDs usable with DesktopClickElement, and frame coordinates for fallback.

Prefer this over Screenshot when you know what you're looking for - it's structured, token-efficient, and exact. Fallback to Screenshot only when AX data is missing (games, canvases, some Electron apps).

Query modes: 'fuzzy' (default, Levenshtein-tolerant), 'substring', 'exact'. Use 'role' to narrow by element type. Use 'contains_text' for broad matches.`
}

// InputSchema returns the JSON Schema for DesktopFindElement.
func (t *DesktopFindElementTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"app":           map[string]any{"type": "string", "description": "Bundle ID or app name. Omit for frontmost."},
			"role":          map[string]any{"type": "string", "description": "AX role like AXButton, AXTextField, AXLink."},
			"title":         map[string]any{"type": "string"},
			"text":          map[string]any{"type": "string", "description": "Matches title, label, or value (case-insensitive)."},
			"contains_text": map[string]any{"type": "string"},
			"descendant_of": map[string]any{"type": "string", "description": "AX element ID - scope query to subtree."},
			"max_results":   map[string]any{"type": "integer", "description": "Default 1."},
			"mode":          map[string]any{"type": "string", "enum": []string{"exact", "substring", "fuzzy"}, "description": "Default fuzzy."},
		},
	}
}

// Execute implements Tool.
func (t *DesktopFindElementTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	if runtime.GOOS != "darwin" {
		return ToolResult{Content: "DesktopFindElement only available on macOS", IsError: true}
	}

	q, err := decodeAXQuery(input)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}
	}

	result, err := t.bridge.AXFind(ctx, q)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("ax_find failed: %v", err), IsError: true}
	}

	matches := result.Matches
	if matches == nil {
		matches = []macos.AXNode{}
	}

	out, err := json.Marshal(matches)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to encode result: %v", err), IsError: true}
	}

	return ToolResult{Content: string(out)}
}

// --- Helpers ---

func decodeAXQuery(input map[string]any) (macos.AXQuery, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return macos.AXQuery{}, fmt.Errorf("marshal input: %w", err)
	}
	var q macos.AXQuery
	if err := json.Unmarshal(raw, &q); err != nil {
		return macos.AXQuery{}, fmt.Errorf("decode query: %w", err)
	}
	return q, nil
}
