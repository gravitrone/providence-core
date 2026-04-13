package tools

import (
	"context"
	"fmt"
	"runtime"

	"github.com/gravitrone/providence-core/internal/bridge/macos"
)

// ClipboardTool reads and writes the system clipboard.
type ClipboardTool struct {
	bridge *macos.Bridge
}

// NewClipboardTool creates a new clipboard tool.
func NewClipboardTool(bridge *macos.Bridge) *ClipboardTool {
	return &ClipboardTool{bridge: bridge}
}

func (t *ClipboardTool) Name() string { return "Clipboard" }
func (t *ClipboardTool) Description() string {
	return "Read or write the system clipboard."
}
func (t *ClipboardTool) ReadOnly() bool { return false }

// Prompt implements ToolPrompter.
func (t *ClipboardTool) Prompt() string {
	return `Read or write the system clipboard (macOS only). Use action "read" to get current clipboard contents, "write" to set new contents.`
}

func (t *ClipboardTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action: read or write.",
				"enum":        []string{"read", "write"},
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Text to write to clipboard (required for write action).",
			},
		},
		"required": []string{"action"},
	}
}

func (t *ClipboardTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	if runtime.GOOS != "darwin" {
		return ToolResult{Content: "clipboard only available on macOS", IsError: true}
	}

	action := paramString(input, "action", "")

	switch action {
	case "read":
		content, err := t.bridge.ClipboardRead(ctx)
		if err != nil {
			return ToolResult{Content: fmt.Sprintf("clipboard read failed: %v", err), IsError: true}
		}
		if content == "" {
			return ToolResult{Content: "(clipboard is empty)"}
		}
		return ToolResult{Content: content}

	case "write":
		text := paramString(input, "text", "")
		if text == "" {
			return ToolResult{Content: "text is required for write action", IsError: true}
		}
		if err := t.bridge.ClipboardWrite(ctx, text); err != nil {
			return ToolResult{Content: fmt.Sprintf("clipboard write failed: %v", err), IsError: true}
		}
		return ToolResult{Content: fmt.Sprintf("Written %d bytes to clipboard", len(text))}

	default:
		return ToolResult{Content: fmt.Sprintf("unknown action: %s (use read or write)", action), IsError: true}
	}
}
