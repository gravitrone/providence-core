package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"time"

	"github.com/gravitrone/providence-core/internal/bridge/macos"
)

// DesktopClickElementTool finds a UI element by query and clicks it via AX actions.
type DesktopClickElementTool struct {
	bridge AXBridge
}

// NewDesktopClickElementTool creates a new DesktopClickElement tool.
func NewDesktopClickElementTool(b AXBridge) *DesktopClickElementTool {
	return &DesktopClickElementTool{bridge: b}
}

func (t *DesktopClickElementTool) Name() string { return "DesktopClickElement" }
func (t *DesktopClickElementTool) Description() string {
	return "Find a UI element by semantic query and click it via Accessibility actions, with optional coordinate fallback."
}
func (t *DesktopClickElementTool) ReadOnly() bool { return false }

// Prompt implements ToolPrompter.
func (t *DesktopClickElementTool) Prompt() string {
	return `Use DesktopClickElement to click UI elements by semantics rather than raw coordinates. Provide a query (role, title, text) to locate the element; the tool resolves it via AXPerform for reliability. Supply fallback_coords if the element might be missing from the AX tree (canvases, games).`
}

// InputSchema returns the JSON Schema for DesktopClickElement.
func (t *DesktopClickElementTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "object",
				"description": "AX query to locate the element.",
				"properties": map[string]any{
					"app":           map[string]any{"type": "string"},
					"role":          map[string]any{"type": "string"},
					"title":         map[string]any{"type": "string"},
					"text":          map[string]any{"type": "string"},
					"contains_text": map[string]any{"type": "string"},
					"descendant_of": map[string]any{"type": "string"},
					"max_results":   map[string]any{"type": "integer"},
					"mode":          map[string]any{"type": "string", "enum": []string{"exact", "substring", "fuzzy"}},
				},
			},
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"click", "double_click", "right_click"},
				"description": "Action to perform. Default click.",
			},
			"fallback_coords": map[string]any{
				"type":        "object",
				"description": "Coordinate fallback if no AX match.",
				"properties": map[string]any{
					"x": map[string]any{"type": "integer"},
					"y": map[string]any{"type": "integer"},
				},
				"required": []string{"x", "y"},
			},
		},
		"required": []string{"query"},
	}
}

type clickElementResult struct {
	MatchedElement *macos.AXNode `json:"matched_element,omitempty"`
	Method         string        `json:"method"`
	DurationMs     int64         `json:"duration_ms"`
}

// Execute implements Tool.
func (t *DesktopClickElementTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	if runtime.GOOS != "darwin" {
		return ToolResult{Content: "DesktopClickElement only available on macOS", IsError: true}
	}

	start := time.Now()
	action := paramString(input, "action", "click")

	// Decode query.
	queryRaw, ok := input["query"]
	if !ok {
		return ToolResult{Content: "query is required", IsError: true}
	}
	queryMap, ok := queryRaw.(map[string]any)
	if !ok {
		return ToolResult{Content: "query must be an object", IsError: true}
	}

	q, err := decodeAXQuery(queryMap)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid query: %v", err), IsError: true}
	}
	q.MaxResults = 1

	findResult, err := t.bridge.AXFind(ctx, q)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("ax_find failed: %v", err), IsError: true}
	}

	if len(findResult.Matches) > 0 {
		match := findResult.Matches[0]
		axErr := t.performAXAction(ctx, match.ID, action)
		if axErr == nil {
			return t.encodeResult(&match, "ax_perform", start)
		}
		// AX action failed; try coordinate fallback at element frame center.
		cx := match.Frame.X + match.Frame.W/2
		cy := match.Frame.Y + match.Frame.H/2
		if coordErr := t.performCoordAction(ctx, cx, cy, action); coordErr != nil {
			return ToolResult{Content: fmt.Sprintf("ax_perform failed (%v) and coord fallback failed: %v", axErr, coordErr), IsError: true}
		}
		return t.encodeResult(&match, "ax_perform", start)
	}

	// No AX match - try fallback_coords.
	if fb, ok := input["fallback_coords"].(map[string]any); ok {
		x := paramInt(fb, "x", -1)
		y := paramInt(fb, "y", -1)
		if x < 0 || y < 0 {
			return ToolResult{Content: "fallback_coords requires non-negative x and y", IsError: true}
		}
		if err := t.performCoordAction(ctx, x, y, action); err != nil {
			return ToolResult{Content: fmt.Sprintf("fallback click failed: %v", err), IsError: true}
		}
		return t.encodeResult(nil, "fallback_coords", start)
	}

	return ToolResult{Content: "no matching element found and no fallback_coords provided", IsError: true}
}

func (t *DesktopClickElementTool) performAXAction(ctx context.Context, elementID, action string) error {
	switch action {
	case "click":
		return t.bridge.AXPerform(ctx, elementID, "AXPress")
	case "right_click":
		return t.bridge.AXPerform(ctx, elementID, "AXShowMenu")
	case "double_click":
		if err := t.bridge.AXPerform(ctx, elementID, "AXPress"); err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)
		return t.bridge.AXPerform(ctx, elementID, "AXPress")
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
}

func (t *DesktopClickElementTool) performCoordAction(ctx context.Context, x, y int, action string) error {
	switch action {
	case "click", "right_click":
		return t.bridge.Click(ctx, x, y)
	case "double_click":
		return t.bridge.DoubleClick(ctx, x, y)
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
}

func (t *DesktopClickElementTool) encodeResult(match *macos.AXNode, method string, start time.Time) ToolResult {
	res := clickElementResult{
		MatchedElement: match,
		Method:         method,
		DurationMs:     time.Since(start).Milliseconds(),
	}
	out, err := json.Marshal(res)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to encode result: %v", err), IsError: true}
	}
	return ToolResult{Content: string(out)}
}
