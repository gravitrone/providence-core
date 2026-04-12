package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// TodoItem represents a single task in the structured todo list.
type TodoItem struct {
	ID         string `json:"id"`
	Content    string `json:"content"`
	ActiveForm string `json:"activeForm"`
	Status     string `json:"status"`
	Priority   int    `json:"priority"`
	ParentID   string `json:"parentId"`
}

var (
	currentTodos []TodoItem
	todoMu       sync.Mutex
)

// TodoWriteTool manages a structured task list with full-replacement semantics.
type TodoWriteTool struct{}

// NewTodoWriteTool creates a TodoWriteTool.
func NewTodoWriteTool() *TodoWriteTool { return &TodoWriteTool{} }

func (t *TodoWriteTool) Name() string { return "TodoWrite" }
func (t *TodoWriteTool) Description() string {
	return "Manage a structured task list. Send the FULL list each call (replacement, not delta). Exactly one task must be in_progress."
}
func (t *TodoWriteTool) ReadOnly() bool { return false }

func (t *TodoWriteTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"todos": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":         map[string]any{"type": "string"},
						"content":    map[string]any{"type": "string", "description": "Imperative form: 'Fix auth bug'"},
						"activeForm": map[string]any{"type": "string", "description": "Present continuous: 'Fixing auth bug'"},
						"status": map[string]any{
							"type": "string",
							"enum": []string{"pending", "in_progress", "completed", "failed", "blocked"},
						},
						"priority": map[string]any{"type": "integer", "description": "0=none, 1=low, 2=med, 3=high"},
						"parentId": map[string]any{"type": "string", "description": "Empty string for root tasks"},
					},
					"required": []string{"id", "content", "status"},
				},
			},
		},
		"required": []string{"todos"},
	}
}

func (t *TodoWriteTool) Execute(_ context.Context, input map[string]any) ToolResult {
	rawTodos, ok := input["todos"]
	if !ok {
		return ToolResult{Content: "missing required field: todos", IsError: true}
	}

	// Marshal and unmarshal to parse the dynamic input into typed structs.
	raw, err := json.Marshal(rawTodos)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to serialize todos: %v", err), IsError: true}
	}

	var items []TodoItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to parse todos: %v", err), IsError: true}
	}

	// Check if all completed - if so, clear.
	allCompleted := len(items) > 0
	for _, item := range items {
		if item.Status != "completed" {
			allCompleted = false
			break
		}
	}

	if allCompleted {
		todoMu.Lock()
		currentTodos = nil
		todoMu.Unlock()
		return ToolResult{Content: "All tasks completed. List cleared."}
	}

	// Validate exactly one in_progress.
	inProgress := 0
	for _, item := range items {
		if item.Status == "in_progress" {
			inProgress++
		}
	}

	if inProgress == 0 {
		return ToolResult{
			Content: "exactly one task must have status \"in_progress\", but found 0. please set one task to in_progress.",
			IsError: true,
		}
	}
	if inProgress > 1 {
		return ToolResult{
			Content: fmt.Sprintf("exactly one task must have status \"in_progress\", but found %d. please set only one task to in_progress.", inProgress),
			IsError: true,
		}
	}

	// Count statuses.
	pending, completed := 0, 0
	for _, item := range items {
		switch item.Status {
		case "pending":
			pending++
		case "completed":
			completed++
		}
	}

	todoMu.Lock()
	currentTodos = items
	todoMu.Unlock()

	return ToolResult{
		Content: fmt.Sprintf("Tasks updated successfully. %d pending, 1 in progress, %d completed.", pending, completed),
	}
}

// GetCurrentTodos returns a copy of the current todo list.
func GetCurrentTodos() []TodoItem {
	todoMu.Lock()
	defer todoMu.Unlock()
	out := make([]TodoItem, len(currentTodos))
	copy(out, currentTodos)
	return out
}

// ResetTodos clears the todo list (used in tests).
func ResetTodos() {
	todoMu.Lock()
	currentTodos = nil
	todoMu.Unlock()
}
