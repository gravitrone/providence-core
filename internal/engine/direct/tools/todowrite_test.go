package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTodoWriteValidInProgress(t *testing.T) {
	ResetTodos()
	tool := NewTodoWriteTool()

	result := tool.Execute(context.Background(), map[string]any{
		"todos": []any{
			map[string]any{"id": "1", "content": "Fix auth bug", "activeForm": "Fixing auth bug", "status": "in_progress", "priority": 3, "parentId": ""},
			map[string]any{"id": "2", "content": "Add tests", "activeForm": "Adding tests", "status": "pending", "priority": 1, "parentId": ""},
		},
	})

	require.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "Tasks updated successfully")
	assert.Contains(t, result.Content, "1 pending")
	assert.Contains(t, result.Content, "1 in progress")
	assert.Contains(t, result.Content, "0 completed")

	todos := GetCurrentTodos()
	assert.Len(t, todos, 2)
}

func TestTodoWriteNoInProgress(t *testing.T) {
	ResetTodos()
	tool := NewTodoWriteTool()

	result := tool.Execute(context.Background(), map[string]any{
		"todos": []any{
			map[string]any{"id": "1", "content": "Fix bug", "status": "pending"},
			map[string]any{"id": "2", "content": "Add tests", "status": "pending"},
		},
	})

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "exactly one task must have status")
	assert.Contains(t, result.Content, "found 0")
}

func TestTodoWriteMultipleInProgress(t *testing.T) {
	ResetTodos()
	tool := NewTodoWriteTool()

	result := tool.Execute(context.Background(), map[string]any{
		"todos": []any{
			map[string]any{"id": "1", "content": "Fix bug", "status": "in_progress"},
			map[string]any{"id": "2", "content": "Add tests", "status": "in_progress"},
		},
	})

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "exactly one task must have status")
	assert.Contains(t, result.Content, "found 2")
}

func TestTodoWriteAllCompleted(t *testing.T) {
	ResetTodos()
	tool := NewTodoWriteTool()

	// First set some todos.
	tool.Execute(context.Background(), map[string]any{
		"todos": []any{
			map[string]any{"id": "1", "content": "Fix bug", "status": "in_progress"},
		},
	})
	require.Len(t, GetCurrentTodos(), 1)

	// Now mark all completed - list should clear.
	result := tool.Execute(context.Background(), map[string]any{
		"todos": []any{
			map[string]any{"id": "1", "content": "Fix bug", "status": "completed"},
		},
	})

	require.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "All tasks completed")
	assert.Empty(t, GetCurrentTodos())
}

func TestTodoWriteWithSubtasks(t *testing.T) {
	ResetTodos()
	tool := NewTodoWriteTool()

	result := tool.Execute(context.Background(), map[string]any{
		"todos": []any{
			map[string]any{"id": "1", "content": "Build auth", "status": "pending", "parentId": ""},
			map[string]any{"id": "1a", "content": "Design schema", "status": "in_progress", "parentId": "1"},
			map[string]any{"id": "1b", "content": "Write handlers", "status": "pending", "parentId": "1"},
		},
	})

	require.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "2 pending")
	assert.Contains(t, result.Content, "1 in progress")

	todos := GetCurrentTodos()
	assert.Len(t, todos, 3)

	// Verify parent-child relationships.
	assert.Equal(t, "", todos[0].ParentID)
	assert.Equal(t, "1", todos[1].ParentID)
	assert.Equal(t, "1", todos[2].ParentID)
}
