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
	return "Send a message to another running agent by ID or name. Use to=\"*\" to broadcast to all running agents. Supports structured message types: text (default), shutdown_request, plan_approval. If the target agent has completed, it will be auto-resumed in the background with the message as its new prompt."
}
func (t *SendMessageTool) ReadOnly() bool { return true }

// Prompt implements ToolPrompter with CC-parity guidance for inter-agent messaging.
func (t *SendMessageTool) Prompt() string {
	return `Send a message to another agent.

| to value | meaning |
|---|---|
| "researcher" | Agent by name |
| "*" | Broadcast to all agents - expensive (linear in team size), use only when everyone genuinely needs it |

Your plain text output is NOT visible to other agents - to communicate, you MUST call this tool. Messages from agents are delivered automatically; you don't check an inbox. Refer to agents by name, never by UUID. When relaying, don't quote the original - it's already rendered to the user.

To continue a previously spawned agent, use SendMessage with the agent's name as the "to" field instead of spawning a new one. The agent resumes with its full context preserved.`
}

func (t *SendMessageTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"to": map[string]any{
				"type":        "string",
				"description": "Agent ID (e.g. agent-abc12345), agent name, or \"*\" to broadcast to all running agents",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "The message text to deliver to the target agent",
			},
			"type": map[string]any{
				"type":        "string",
				"enum":        []string{"text", "shutdown_request", "plan_approval"},
				"description": "Message type: text (default), shutdown_request (graceful stop), plan_approval (approve a pending plan)",
			},
		},
		"required": []string{"to", "message"},
	}
}

func (t *SendMessageTool) Execute(_ context.Context, input map[string]any) ToolResult {
	to := paramString(input, "to", "")
	message := paramString(input, "message", "")
	msgType := paramString(input, "type", "text")

	if to == "" || message == "" {
		return ToolResult{Content: "both 'to' and 'message' are required", IsError: true}
	}

	// Format message with type prefix for non-text messages.
	formattedMsg := message
	switch msgType {
	case "shutdown_request":
		formattedMsg = "[SHUTDOWN_REQUEST] " + message
	case "plan_approval":
		formattedMsg = "[PLAN_APPROVED] " + message
	}

	// Broadcast: send to all running agents.
	if to == "*" {
		return t.broadcast(formattedMsg)
	}

	// Single target: try by ID first, then by name.
	return t.sendToSingle(to, formattedMsg)
}

// broadcast sends a message to all running agents and returns a summary.
func (t *SendMessageTool) broadcast(message string) ToolResult {
	agents := t.runner.List()

	delivered := 0
	skipped := 0
	var errors []string

	for _, agent := range agents {
		if agent.Status != "running" {
			skipped++
			continue
		}
		if err := t.runner.SendTo(agent.ID, message); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", agent.ID, err))
			continue
		}
		delivered++
	}

	resp := map[string]any{
		"status":    "broadcast_complete",
		"delivered": delivered,
		"skipped":   skipped,
	}
	if len(errors) > 0 {
		resp["errors"] = errors
	}
	raw, _ := json.Marshal(resp)
	return ToolResult{Content: string(raw)}
}

// sendToSingle sends a message to a single agent by ID or name.
// If the target agent has completed, it returns a notice about auto-resume
// (actual re-spawn requires the executor which lives in the engine layer,
// so we surface the intent for the caller to handle).
func (t *SendMessageTool) sendToSingle(to, message string) ToolResult {
	// Try by ID first.
	err := t.runner.SendTo(to, message)
	if err == nil {
		resp := map[string]string{
			"status":   "delivered",
			"agent_id": to,
		}
		raw, _ := json.Marshal(resp)
		return ToolResult{Content: string(raw)}
	}

	// Try finding by name.
	agent := t.runner.FindByName(to)
	if agent == nil {
		return ToolResult{Content: fmt.Sprintf("agent not found: %s", to), IsError: true}
	}

	// If agent completed/failed/killed, signal auto-resume intent.
	if agent.Status != "running" {
		resp := map[string]any{
			"status":       "agent_completed",
			"agent_id":     agent.ID,
			"agent_name":   agent.Name,
			"agent_status": agent.Status,
			"message":      "Target agent is not running. Use the Agent tool to re-spawn it with the message as the new prompt.",
			"auto_resume":  true,
		}
		raw, _ := json.Marshal(resp)
		return ToolResult{Content: string(raw)}
	}

	err = t.runner.SendTo(agent.ID, message)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to send: %v", err), IsError: true}
	}

	resp := map[string]string{
		"status":   "delivered",
		"agent_id": agent.ID,
	}
	raw, _ := json.Marshal(resp)
	return ToolResult{Content: string(raw)}
}
