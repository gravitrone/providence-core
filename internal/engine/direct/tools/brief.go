package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gravitrone/providence-core/internal/engine/session"
)

// BriefTool emits proactive notifications to the TUI without a full response.
// Used by Ember/background agents to inform the user of status updates.
type BriefTool struct {
	bus *session.Bus
}

// NewBriefTool creates a BriefTool wired to the session event bus.
func NewBriefTool(bus *session.Bus) *BriefTool {
	return &BriefTool{bus: bus}
}

func (b *BriefTool) Name() string { return "Brief" }
func (b *BriefTool) Description() string {
	return "Emit a proactive notification to the user without a full response. Use for status updates, progress reports, and non-blocking alerts from background agents."
}
func (b *BriefTool) ReadOnly() bool { return false }

func (b *BriefTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message": map[string]any{
				"type":        "string",
				"description": "The notification message to display to the user",
			},
			"status": map[string]any{
				"type":        "string",
				"enum":        []string{"proactive", "blocking", "info"},
				"description": "Notification urgency: proactive (background update), blocking (needs attention), info (informational)",
			},
		},
		"required": []string{"message", "status"},
	}
}

// Prompt implements ToolPrompter.
func (b *BriefTool) Prompt() string {
	return `Emit a short notification to the user. Use this instead of a full response when you want to report progress or alert the user from a background task.

Status levels:
- proactive: background status update, displayed as a subtle notification
- blocking: requires user attention, displayed prominently
- info: informational, no action needed`
}

// Execute publishes a brief event to the session bus.
func (b *BriefTool) Execute(_ context.Context, input map[string]any) ToolResult {
	message := paramString(input, "message", "")
	status := paramString(input, "status", "info")

	if message == "" {
		return ToolResult{Content: "message is required", IsError: true}
	}

	validStatuses := map[string]bool{"proactive": true, "blocking": true, "info": true}
	if !validStatuses[status] {
		return ToolResult{
			Content: fmt.Sprintf("invalid status %q: must be proactive, blocking, or info", status),
			IsError: true,
		}
	}

	if b.bus != nil {
		b.bus.Publish(session.Event{
			Type: "brief",
			Data: map[string]string{
				"message": message,
				"status":  status,
			},
		})
	}

	resp, _ := json.Marshal(map[string]string{
		"status":  "delivered",
		"message": message,
		"level":   status,
	})
	return ToolResult{Content: string(resp)}
}
