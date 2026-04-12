package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gravitrone/providence-core/internal/engine/subagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockTaskExecutor(result string) subagent.Executor {
	return func(_ context.Context, _ string, _ subagent.AgentType) (string, error) {
		return result, nil
	}
}

func TestTaskToolSync(t *testing.T) {
	runner := subagent.NewRunner()
	tool := NewTaskTool(runner, mockTaskExecutor("task completed successfully"))

	result := tool.Execute(context.Background(), map[string]any{
		"description": "test task",
		"prompt":      "do something useful",
	})

	require.False(t, result.IsError, result.Content)
	assert.Equal(t, "task completed successfully", result.Content)
}

func TestTaskToolSyncWithType(t *testing.T) {
	runner := subagent.NewRunner()
	tool := NewTaskTool(runner, mockTaskExecutor("reviewed"))

	result := tool.Execute(context.Background(), map[string]any{
		"description":  "review code",
		"prompt":       "review main.go",
		"subagent_type": "review",
	})

	require.False(t, result.IsError, result.Content)
	assert.Equal(t, "reviewed", result.Content)
}

func TestTaskToolAsync(t *testing.T) {
	runner := subagent.NewRunner()
	tool := NewTaskTool(runner, mockTaskExecutor("bg result"))

	result := tool.Execute(context.Background(), map[string]any{
		"description":      "background task",
		"prompt":           "do in background",
		"run_in_background": true,
	})

	require.False(t, result.IsError, result.Content)

	var resp map[string]string
	err := json.Unmarshal([]byte(result.Content), &resp)
	require.NoError(t, err)
	assert.Equal(t, "async_launched", resp["status"])
	assert.Contains(t, resp["agent_id"], "agent-")
	assert.Equal(t, "background task", resp["description"])
}

func TestTaskToolMissingFields(t *testing.T) {
	runner := subagent.NewRunner()
	tool := NewTaskTool(runner, mockTaskExecutor(""))

	tests := []struct {
		name  string
		input map[string]any
	}{
		{"missing both", map[string]any{}},
		{"missing prompt", map[string]any{"description": "test"}},
		{"missing description", map[string]any{"prompt": "test"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tool.Execute(context.Background(), tc.input)
			assert.True(t, result.IsError)
			assert.Contains(t, result.Content, "required")
		})
	}
}

func TestTaskNotificationXML(t *testing.T) {
	n := subagent.TaskNotification{
		TaskID:   "agent-abc123",
		Status:   "completed",
		Summary:  "Fixed the bug",
		Result:   "Applied patch to main.go",
		Tokens:   1500,
		ToolUses: 3,
		Duration: 4200,
	}

	xml := n.ToXML()
	assert.True(t, strings.Contains(xml, "<task-id>agent-abc123</task-id>"))
	assert.True(t, strings.Contains(xml, "<status>completed</status>"))
	assert.True(t, strings.Contains(xml, "<summary>Fixed the bug</summary>"))
	assert.True(t, strings.Contains(xml, "<result>Applied patch to main.go</result>"))
	assert.True(t, strings.Contains(xml, "<total_tokens>1500</total_tokens>"))
	assert.True(t, strings.Contains(xml, "<tool_uses>3</tool_uses>"))
	assert.True(t, strings.Contains(xml, "<duration_ms>4200</duration_ms>"))
}

func TestTaskToolName(t *testing.T) {
	runner := subagent.NewRunner()
	tool := NewTaskTool(runner, mockTaskExecutor(""))
	assert.Equal(t, "Task", tool.Name())
	assert.NotEmpty(t, tool.Description())
	assert.True(t, tool.ReadOnly())
}

func TestTaskToolInputSchema(t *testing.T) {
	runner := subagent.NewRunner()
	tool := NewTaskTool(runner, mockTaskExecutor(""))
	schema := tool.InputSchema()
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, props, "description")
	assert.Contains(t, props, "prompt")
	assert.Contains(t, props, "run_in_background")
}
