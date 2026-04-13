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

// TodoWriteTool manages a structured task list with full-replacement semantics.
type TodoWriteTool struct {
	todos []TodoItem
	mu    sync.Mutex
}

// NewTodoWriteTool creates a TodoWriteTool.
func NewTodoWriteTool() *TodoWriteTool { return &TodoWriteTool{} }

func (t *TodoWriteTool) Name() string { return "TodoWrite" }
func (t *TodoWriteTool) Description() string {
	return "Manage a structured task list. Send the FULL list each call (replacement, not delta). Exactly one task must be in_progress."
}
func (t *TodoWriteTool) ReadOnly() bool { return false }

// Prompt implements ToolPrompter with CC-parity guidance for todo tracking.
func (t *TodoWriteTool) Prompt() string {
	return `Use this tool to create and manage a structured task list for your current session. This helps you track progress, organize complex tasks, and demonstrate thoroughness to the user.

When to use:
- Complex multi-step tasks (3+ distinct steps)
- Non-trivial tasks requiring careful planning
- User explicitly requests todo list
- User provides multiple tasks (numbered or comma-separated)
- After receiving new instructions - capture requirements as todos immediately
- Mark tasks in_progress BEFORE beginning work. Only one task in_progress at a time.
- Mark tasks completed IMMEDIATELY after finishing, not in batches.

When NOT to use:
- Single, straightforward task
- Trivial task completable in under 3 steps
- Purely conversational or informational response

Task states: pending, in_progress, completed.
Task descriptions must have two forms:
- content: imperative form ("Run tests", "Build the project")
- activeForm: present continuous form shown during execution ("Running tests", "Building the project")

ONLY mark a task completed when FULLY accomplished. If you hit errors or blockers, keep as in_progress.`
}

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
		t.mu.Lock()
		t.todos = nil
		t.mu.Unlock()
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

	t.mu.Lock()
	t.todos = items
	t.mu.Unlock()

	return ToolResult{
		Content: fmt.Sprintf("Tasks updated successfully. %d pending, 1 in progress, %d completed.", pending, completed),
	}
}

// GetCurrentTodos returns a copy of the current todo list.
func (t *TodoWriteTool) GetCurrentTodos() []TodoItem {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]TodoItem, len(t.todos))
	copy(out, t.todos)
	return out
}

// ResetTodos clears the todo list (used in tests).
func (t *TodoWriteTool) ResetTodos() {
	t.mu.Lock()
	t.todos = nil
	t.mu.Unlock()
}
