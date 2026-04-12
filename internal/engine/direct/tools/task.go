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

func (t *TaskTool) Name() string { return "Task" }
func (t *TaskTool) Description() string {
	return "Launch a subagent to perform a task. The agent runs in its own goroutine with its own system prompt and tool set. Use run_in_background=true for async execution."
}
func (t *TaskTool) ReadOnly() bool { return true } // launching is read-only, the agent does writes

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
			"merge_strategy": map[string]any{
				"type":        "string",
				"enum":        []string{"auto", "manual", "vote"},
				"description": "How to merge results from /fork agents",
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
		RunInBG:       paramBool(input, "run_in_background", false),
		Name:          paramString(input, "name", ""),
		Tools:         paramString(input, "tools", ""),
		MergeStrategy: paramString(input, "merge_strategy", ""),
	}

	// Resolve agent type.
	agentType := t.resolveAgentType(taskInput.SubagentType)

	// Override model/engine if specified in input.
	if taskInput.Model != "" {
		agentType.Model = taskInput.Model
	}
	if taskInput.Engine != "" {
		agentType.Engine = taskInput.Engine
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
