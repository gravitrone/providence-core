package tools

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnterPlanMode(t *testing.T) {
	state := NewPlanModeState(nil)
	enter := NewEnterPlanModeTool(state)

	assert.False(t, state.IsActive())

	result := enter.Execute(context.Background(), nil)
	require.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "Plan mode activated")
	assert.True(t, state.IsActive())

	// Entering again should error.
	result = enter.Execute(context.Background(), nil)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "already active")
}

func TestExitPlanModeApproved(t *testing.T) {
	state := NewPlanModeState(nil)
	enter := NewEnterPlanModeTool(state)
	exit := NewExitPlanModeTool(state)

	// Enter plan mode first.
	enter.Execute(context.Background(), nil)
	require.True(t, state.IsActive())

	// Run ExitPlanMode in a goroutine since it blocks.
	resultCh := make(chan ToolResult, 1)
	go func() {
		resultCh <- exit.Execute(context.Background(), map[string]any{
			"plan": "1. Refactor auth\n2. Add tests\n3. Deploy",
		})
	}()

	// Give the goroutine time to set up the channel.
	time.Sleep(50 * time.Millisecond)

	// Approve the plan.
	state.ApprovePlan(true)

	result := <-resultCh
	require.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "approved")
	assert.Contains(t, result.Content, "Refactor auth")
	assert.False(t, state.IsActive())
}

func TestExitPlanModeRejected(t *testing.T) {
	state := NewPlanModeState(nil)
	enter := NewEnterPlanModeTool(state)
	exit := NewExitPlanModeTool(state)

	enter.Execute(context.Background(), nil)
	require.True(t, state.IsActive())

	resultCh := make(chan ToolResult, 1)
	go func() {
		resultCh <- exit.Execute(context.Background(), map[string]any{
			"plan": "bad plan",
		})
	}()

	time.Sleep(50 * time.Millisecond)
	state.ApprovePlan(false)

	result := <-resultCh
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "rejected")
	assert.False(t, state.IsActive())
}

func TestExitPlanModeNotActive(t *testing.T) {
	state := NewPlanModeState(nil)
	exit := NewExitPlanModeTool(state)

	result := exit.Execute(context.Background(), map[string]any{
		"plan": "some plan",
	})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "not active")
}
