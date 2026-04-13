package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gravitrone/providence-core/internal/engine/subagent"
)

// SendMessageTool allows agents to send messages to other running agents.
type SendMessageTool struct {
	runner *subagent.Runner
}

// NewSendMessageTool creates a SendMessageTool wired to the given runner.
func NewSendMessageTool(runner *subagent.Runner) *SendMessageTool {
	return &SendMessageTool{runner: runner}
}

func (t *SendMessageTool) Name() string { return "SendMessage" }
func (t *SendMessageTool) Description() string {
	return "Send a message to another running agent by ID or name. The message is queued in the target agent's inbox."
}
func (t *SendMessageTool) ReadOnly() bool { return true }

func (t *SendMessageTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"to": map[string]any{
				"type":        "string",
				"description": "Agent ID (e.g. agent-abc12345) or agent name to send the message to",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "The message text to deliver to the target agent",
			},
		},
		"required": []string{"to", "message"},
	}
}

func (t *SendMessageTool) Execute(_ context.Context, input map[string]any) ToolResult {
	to := paramString(input, "to", "")
	message := paramString(input, "message", "")

	if to == "" || message == "" {
		return ToolResult{Content: "both 'to' and 'message' are required", IsError: true}
	}

	// Try by ID first, then by name.
	err := t.runner.SendTo(to, message)
	if err != nil {
		// Try finding by name.
		agent := t.runner.FindByName(to)
		if agent == nil {
			return ToolResult{Content: fmt.Sprintf("agent not found: %s", to), IsError: true}
		}
		err = t.runner.SendTo(agent.ID, message)
		if err != nil {
			return ToolResult{Content: fmt.Sprintf("failed to send: %v", err), IsError: true}
		}
		to = agent.ID
	}

	resp := map[string]string{
		"status":   "delivered",
		"agent_id": to,
	}
	raw, _ := json.Marshal(resp)
	return ToolResult{Content: string(raw)}
}
