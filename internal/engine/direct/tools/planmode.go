package tools

import (
	"context"
	"encoding/json"
	"sync"
)

// PlanModeState tracks whether plan mode is active.
type PlanModeState struct {
	mu       sync.Mutex
	active   bool
	eventFn  func(string, any)
	approveCh chan bool
}

// NewPlanModeState creates a shared plan mode state with an event emitter callback.
func NewPlanModeState(eventFn func(string, any)) *PlanModeState {
	return &PlanModeState{eventFn: eventFn}
}

// IsActive returns true if plan mode is currently active.
func (p *PlanModeState) IsActive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

// ApprovePlan is called by the UI to approve or reject the plan.
func (p *PlanModeState) ApprovePlan(approved bool) {
	p.mu.Lock()
	ch := p.approveCh
	p.mu.Unlock()

	if ch != nil {
		ch <- approved
	}
}

// --- EnterPlanModeTool ---

// EnterPlanModeTool activates plan mode, restricting tools to read-only.
type EnterPlanModeTool struct {
	state *PlanModeState
}

// NewEnterPlanModeTool creates an EnterPlanModeTool.
func NewEnterPlanModeTool(state *PlanModeState) *EnterPlanModeTool {
	return &EnterPlanModeTool{state: state}
}

func (e *EnterPlanModeTool) Name() string { return "EnterPlanMode" }
func (e *EnterPlanModeTool) Description() string {
	return "Enter plan mode. Write tools are restricted to read-only until ExitPlanMode is called with a plan."
}
func (e *EnterPlanModeTool) ReadOnly() bool { return true }

func (e *EnterPlanModeTool) InputSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (e *EnterPlanModeTool) Execute(_ context.Context, _ map[string]any) ToolResult {
	e.state.mu.Lock()
	if e.state.active {
		e.state.mu.Unlock()
		return ToolResult{Content: "plan mode is already active", IsError: true}
	}
	e.state.active = true
	e.state.mu.Unlock()

	return ToolResult{Content: "Plan mode activated. Write tools are now restricted."}
}

// --- ExitPlanModeTool ---

// ExitPlanModeTool exits plan mode after the user approves the plan.
type ExitPlanModeTool struct {
	state *PlanModeState
}

// NewExitPlanModeTool creates an ExitPlanModeTool.
func NewExitPlanModeTool(state *PlanModeState) *ExitPlanModeTool {
	return &ExitPlanModeTool{state: state}
}

func (x *ExitPlanModeTool) Name() string { return "ExitPlanMode" }
func (x *ExitPlanModeTool) Description() string {
	return "Exit plan mode. Presents the plan for user approval, then restores write permissions."
}
func (x *ExitPlanModeTool) ReadOnly() bool { return false }

func (x *ExitPlanModeTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"plan": map[string]any{
				"type":        "string",
				"description": "The plan text to present for user approval.",
			},
		},
		"required": []string{"plan"},
	}
}

func (x *ExitPlanModeTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	plan := paramString(input, "plan", "")
	if plan == "" {
		return ToolResult{Content: "plan field is required", IsError: true}
	}

	x.state.mu.Lock()
	if !x.state.active {
		x.state.mu.Unlock()
		return ToolResult{Content: "plan mode is not active", IsError: true}
	}

	// Create approval channel.
	ch := make(chan bool, 1)
	x.state.approveCh = ch
	x.state.mu.Unlock()

	// Emit event to UI with the plan for approval.
	if x.state.eventFn != nil {
		payload := map[string]string{"plan": plan}
		x.state.eventFn("planApproval", payload)
	}

	// Block until approval or context cancellation.
	select {
	case approved := <-ch:
		x.state.mu.Lock()
		x.state.active = false
		x.state.approveCh = nil
		x.state.mu.Unlock()

		if approved {
			result, _ := json.Marshal(map[string]string{"status": "approved", "plan": plan})
			return ToolResult{Content: string(result)}
		}
		return ToolResult{Content: "plan rejected by user", IsError: true}

	case <-ctx.Done():
		x.state.mu.Lock()
		x.state.active = false
		x.state.approveCh = nil
		x.state.mu.Unlock()
		return ToolResult{Content: "plan approval cancelled", IsError: true}
	}
}
