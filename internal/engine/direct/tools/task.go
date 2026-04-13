package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gravitrone/providence-core/internal/engine/subagent"
)

// TaskTool spawns subagent goroutines via the Runner.
type TaskTool struct {
	runner   *subagent.Runner
	executor subagent.Executor
}

// NewTaskTool creates a TaskTool wired to the given runner and executor.
func NewTaskTool(runner *subagent.Runner, executor subagent.Executor) *TaskTool {
	return &TaskTool{runner: runner, executor: executor}
}

func (t *TaskTool) Name() string { return "Agent" }
func (t *TaskTool) Description() string {
	return "Launch a subagent to perform a task. The agent runs in its own goroutine with its own system prompt and tool set. Use run_in_background=true for async execution."
}
func (t *TaskTool) ReadOnly() bool { return true } // launching is read-only, the agent does writes

// Prompt implements ToolPrompter with CC-parity guidance for subagent usage.
func (t *TaskTool) Prompt() string {
	return `Launch a new agent to handle complex, multi-step tasks autonomously.

The Agent tool launches specialized agents (subprocesses) that autonomously handle complex tasks. Each agent type has specific capabilities and tools available to it.

When using the Agent tool, specify a subagent_type parameter to select which agent type to use. If omitted, the general-purpose agent is used.

When NOT to use the Agent tool:
- If you want to read a specific file path, use the Read tool or Glob tool instead, to find the match more quickly
- If you are searching for a specific class definition like "class Foo", use Glob instead, to find the match more quickly
- If you are searching for code within a specific file or set of 2-3 files, use the Read tool instead, to find the match more quickly
- Other tasks that are not related to the agent descriptions above

Usage notes:
- Always include a short description (3-5 words) summarizing what the agent will do
- Launch multiple agents concurrently whenever possible, to maximize performance; to do that, use a single message with multiple tool uses
- When the agent is done, it will return a single message back to you. The result returned by the agent is not visible to the user. To show the user the result, you should send a text message back to the user with a concise summary of the result.
- You can optionally run agents in the background using the run_in_background parameter. When an agent runs in the background, you will be automatically notified when it completes - do NOT sleep, poll, or proactively check on its progress. Continue with other work or respond to the user instead.
- Foreground vs background: Use foreground (default) when you need the agent's results before you can proceed - e.g., research agents whose findings inform your next steps. Use background when you have genuinely independent work to do in parallel.
- To continue a previously spawned agent, use SendMessage with the agent's ID or name as the "to" field. The agent resumes with its full context preserved. Each Agent invocation starts fresh - provide a complete task description.
- The agent's outputs should generally be trusted
- Clearly tell the agent whether you expect it to write code or just to do research (search, file reads, web fetches, etc.), since it is not aware of the user's intent
- If the user specifies that they want you to run agents "in parallel", you MUST send a single message with multiple Agent tool use content blocks.
- You can optionally set isolation: "worktree" to run the agent in a temporary git worktree, giving it an isolated copy of the repository. The worktree is automatically cleaned up if the agent makes no changes; if changes are made, the worktree path and branch are returned in the result.

## Writing the prompt

Brief the agent like a smart colleague who just walked into the room - it hasn't seen this conversation, doesn't know what you've tried, doesn't understand why this task matters.
- Explain what you're trying to accomplish and why.
- Describe what you've already learned or ruled out.
- Give enough context about the surrounding problem that the agent can make judgment calls rather than just following a narrow instruction.
- If you need a short response, say so ("report in under 200 words").
- Lookups: hand over the exact command. Investigations: hand over the question - prescribed steps become dead weight when the premise is wrong.

Terse command-style prompts produce shallow, generic work.

Never delegate understanding. Don't write "based on your findings, fix the bug" or "based on the research, implement it." Those phrases push synthesis onto the agent instead of doing it yourself. Write prompts that prove you understood: include file paths, line numbers, what specifically to change.`
}

// InputSchema returns the JSON Schema for the Task tool input.
func (t *TaskTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"description": map[string]any{
				"type":        "string",
				"description": "Short description of what this agent should accomplish",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "The full prompt/instructions for the subagent",
			},
			"subagent_type": map[string]any{
				"type":        "string",
				"description": "Named agent type: code, research, review, or omit for default",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Model override (default: inherit from parent)",
			},
			"engine": map[string]any{
				"type":        "string",
				"description": "Engine override (default: inherit from parent)",
			},
			"run_in_background": map[string]any{
				"type":        "boolean",
				"description": "If true, launch async and return immediately with agent ID",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Human-readable name for the agent (shown in dashboard)",
			},
			"tools": map[string]any{
				"type":        "string",
				"description": "Comma-separated tool allowlist (default: inherit from agent type)",
			},
			"mode": map[string]any{
				"type":        "string",
				"enum":        []string{"default", "plan", "auto", "deny"},
				"description": "Permission mode for the agent (default: inherit from agent type)",
			},
			"merge_strategy": map[string]any{
				"type":        "string",
				"enum":        []string{"auto", "manual", "vote"},
				"description": "How to merge results from /fork agents",
			},
			"isolation": map[string]any{
				"type":        "string",
				"enum":        []string{"worktree", "docker", "none"},
				"description": "Isolation mode: worktree (git worktree), docker (container), or none (default)",
			},
		},
		"required": []string{"description", "prompt"},
	}
}

// Execute launches a subagent via the runner.
func (t *TaskTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	description := paramString(input, "description", "")
	prompt := paramString(input, "prompt", "")
	if description == "" || prompt == "" {
		return ToolResult{Content: "description and prompt are required", IsError: true}
	}

	taskInput := subagent.TaskInput{
		Description:   description,
		Prompt:        prompt,
		SubagentType:  paramString(input, "subagent_type", ""),
		Model:         paramString(input, "model", ""),
		Engine:        paramString(input, "engine", ""),
		Mode:          paramString(input, "mode", ""),
		RunInBG:       paramBool(input, "run_in_background", false),
		Name:          paramString(input, "name", ""),
		Tools:         paramString(input, "tools", ""),
		MergeStrategy: paramString(input, "merge_strategy", ""),
		Isolation:     paramString(input, "isolation", ""),
	}

	// Resolve agent type.
	agentType := t.resolveAgentType(taskInput.SubagentType)

	// Override model/engine/permission mode if specified in input.
	if taskInput.Model != "" {
		agentType.Model = taskInput.Model
	}
	if taskInput.Engine != "" {
		agentType.Engine = taskInput.Engine
	}
	if taskInput.Mode != "" {
		agentType.PermissionMode = taskInput.Mode
	}

	agentID, err := t.runner.Spawn(ctx, taskInput, agentType, t.executor)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to spawn agent: %v", err), IsError: true}
	}

	if taskInput.RunInBG {
		resp := map[string]string{
			"status":      "async_launched",
			"agent_id":    agentID,
			"description": description,
		}
		raw, _ := json.Marshal(resp)
		return ToolResult{Content: string(raw)}
	}

	// Sync: wait for result.
	result := t.runner.WaitFor(agentID)
	if result == nil {
		return ToolResult{Content: "agent disappeared unexpectedly", IsError: true}
	}

	if result.Status == "failed" {
		return ToolResult{Content: fmt.Sprintf("agent failed: %s", result.Result), IsError: true}
	}

	return ToolResult{Content: result.Result}
}

func (t *TaskTool) resolveAgentType(name string) subagent.AgentType {
	if name == "" {
		return subagent.DefaultAgentType()
	}
	builtins := subagent.BuiltinAgents
	if at, ok := builtins[name]; ok {
		return at
	}
	// Unknown type, fall back to default.
	return subagent.DefaultAgentType()
}
