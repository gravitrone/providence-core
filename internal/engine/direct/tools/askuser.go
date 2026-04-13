package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// AskUserOption is a single selectable option in a question.
type AskUserOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// AskUserQuestionItem is one question presented to the user.
type AskUserQuestionItem struct {
	Question    string          `json:"question"`
	Header      string          `json:"header"`
	MultiSelect bool            `json:"multiSelect"`
	Options     []AskUserOption `json:"options"`
}

// AskUserQuestionTool blocks the agent loop until the user answers.
type AskUserQuestionTool struct {
	mu        sync.Mutex
	answerCh  chan map[string]string
	questions []AskUserQuestionItem
	eventFn   func(name string, payload any) // callback to emit events to UI
}

// NewAskUserQuestionTool creates an AskUserQuestionTool with an event emitter callback.
// The eventFn is called when the tool needs to present questions to the UI.
// Pass nil for v1 stub behavior (will block forever until ProvideAnswer is called).
func NewAskUserQuestionTool(eventFn func(string, any)) *AskUserQuestionTool {
	return &AskUserQuestionTool{
		eventFn: eventFn,
	}
}

func (a *AskUserQuestionTool) Name() string { return "AskUserQuestion" }
func (a *AskUserQuestionTool) Description() string {
	return "Ask the user a question with 2-4 options. Blocks until the user responds."
}
func (a *AskUserQuestionTool) ReadOnly() bool { return false }

// Prompt implements ToolPrompter with guidance for user interaction.
func (a *AskUserQuestionTool) Prompt() string {
	return `Present a structured question to the user with 2-4 selectable options. Blocks the agent loop until the user responds. Use when you need an explicit decision from the user rather than making an assumption.`
}

func (a *AskUserQuestionTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"questions": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"question":    map[string]any{"type": "string"},
						"header":      map[string]any{"type": "string"},
						"multiSelect": map[string]any{"type": "boolean"},
						"options": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"label":       map[string]any{"type": "string"},
									"description": map[string]any{"type": "string"},
								},
								"required": []string{"label"},
							},
						},
					},
					"required": []string{"question", "options"},
				},
			},
		},
		"required": []string{"questions"},
	}
}

func (a *AskUserQuestionTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	rawQuestions, ok := input["questions"]
	if !ok {
		return ToolResult{Content: "missing required field: questions", IsError: true}
	}

	raw, err := json.Marshal(rawQuestions)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to serialize questions: %v", err), IsError: true}
	}

	var questions []AskUserQuestionItem
	if err := json.Unmarshal(raw, &questions); err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to parse questions: %v", err), IsError: true}
	}

	if len(questions) == 0 {
		return ToolResult{Content: "at least one question is required", IsError: true}
	}

	// Create channel for this invocation.
	ch := make(chan map[string]string, 1)

	a.mu.Lock()
	a.answerCh = ch
	a.questions = questions
	a.mu.Unlock()

	// Emit event to UI so it can present the questions.
	if a.eventFn != nil {
		a.eventFn("askUserQuestion", questions)
	}

	// Block until the UI provides an answer or context is cancelled.
	select {
	case answers := <-ch:
		result, _ := json.Marshal(answers)
		return ToolResult{Content: string(result)}
	case <-ctx.Done():
		return ToolResult{Content: "question cancelled", IsError: true}
	}
}

// ProvideAnswer is called by the UI to unblock the tool with the user's answers.
func (a *AskUserQuestionTool) ProvideAnswer(answers map[string]string) {
	a.mu.Lock()
	ch := a.answerCh
	a.mu.Unlock()

	if ch != nil {
		ch <- answers
	}
}

// PendingQuestions returns the current questions waiting for an answer, or nil.
func (a *AskUserQuestionTool) PendingQuestions() []AskUserQuestionItem {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.questions
}
