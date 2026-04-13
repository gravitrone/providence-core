package subagent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockExecutor(result string, err error, delay time.Duration) Executor {
	return func(ctx context.Context, _ string, _ AgentType) (string, error) {
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		return result, err
	}
}

func TestSpawnSync(t *testing.T) {
	r := NewRunner()
	input := TaskInput{
		Description: "test task",
		Prompt:      "do the thing",
	}

	agentID, err := r.Spawn(context.Background(), input, DefaultAgentType(), mockExecutor("done", nil, 0))
	require.NoError(t, err)
	assert.Contains(t, agentID, "agent-")

	agent, ok := r.Get(agentID)
	require.True(t, ok)
	assert.Equal(t, "completed", agent.Status)
	assert.Equal(t, "done", agent.Result.Result)
}

func TestSpawnSyncFailure(t *testing.T) {
	r := NewRunner()
	input := TaskInput{
		Description: "failing task",
		Prompt:      "fail please",
	}

	agentID, err := r.Spawn(context.Background(), input, DefaultAgentType(), mockExecutor("", fmt.Errorf("something broke"), 0))
	require.NoError(t, err)

	agent, ok := r.Get(agentID)
	require.True(t, ok)
	assert.Equal(t, "failed", agent.Status)
	assert.Contains(t, agent.Result.Result, "something broke")
}

func TestSpawnAsync(t *testing.T) {
	r := NewRunner()
	input := TaskInput{
		Description: "async task",
		Prompt:      "do slowly",
		RunInBG:     true,
	}

	agentID, err := r.Spawn(context.Background(), input, DefaultAgentType(), mockExecutor("async done", nil, 50*time.Millisecond))
	require.NoError(t, err)
	assert.Contains(t, agentID, "agent-")

	// Should still be running immediately after spawn.
	agent, ok := r.Get(agentID)
	require.True(t, ok)
	// Status could be running or completed depending on timing, but ID exists.
	assert.NotEmpty(t, agent.ID)

	// Wait for completion.
	result := r.WaitFor(agentID)
	require.NotNil(t, result)
	assert.Equal(t, "completed", result.Status)
	assert.Equal(t, "async done", result.Result)
}

func TestKill(t *testing.T) {
	r := NewRunner()
	input := TaskInput{
		Description: "long task",
		Prompt:      "run forever",
		RunInBG:     true,
	}

	agentID, err := r.Spawn(context.Background(), input, DefaultAgentType(), mockExecutor("", nil, 10*time.Second))
	require.NoError(t, err)

	// Give goroutine a moment to start.
	time.Sleep(10 * time.Millisecond)

	err = r.Kill(agentID)
	require.NoError(t, err)

	result := r.WaitFor(agentID)
	require.NotNil(t, result)
	assert.Equal(t, "killed", result.Status)
}

func TestKillNotFound(t *testing.T) {
	r := NewRunner()
	err := r.Kill("agent-nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestKillAlreadyCompleted(t *testing.T) {
	r := NewRunner()
	input := TaskInput{Description: "quick", Prompt: "done"}
	agentID, _ := r.Spawn(context.Background(), input, DefaultAgentType(), mockExecutor("ok", nil, 0))

	err := r.Kill(agentID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestList(t *testing.T) {
	r := NewRunner()

	// Spawn a few sync agents.
	for i := 0; i < 3; i++ {
		input := TaskInput{Description: fmt.Sprintf("task %d", i), Prompt: "go"}
		_, err := r.Spawn(context.Background(), input, DefaultAgentType(), mockExecutor("ok", nil, 0))
		require.NoError(t, err)
	}

	agents := r.List()
	assert.Len(t, agents, 3)
}

func TestWaitFor(t *testing.T) {
	r := NewRunner()
	input := TaskInput{
		Description: "waited task",
		Prompt:      "wait for me",
		RunInBG:     true,
	}

	agentID, err := r.Spawn(context.Background(), input, DefaultAgentType(), mockExecutor("waited result", nil, 30*time.Millisecond))
	require.NoError(t, err)

	result := r.WaitFor(agentID)
	require.NotNil(t, result)
	assert.Equal(t, "completed", result.Status)
	assert.Equal(t, "waited result", result.Result)
	assert.Greater(t, result.DurationMS, int64(0))
}

func TestWaitForNotFound(t *testing.T) {
	r := NewRunner()
	result := r.WaitFor("agent-nope")
	assert.Nil(t, result)
}

func TestAntiRecursionPrompt(t *testing.T) {
	assert.NotEmpty(t, AntiRecursionPrompt)
	assert.Contains(t, AntiRecursionPrompt, "Do NOT spawn sub-agents")
}

func TestStrippedAgentPrompt(t *testing.T) {
	assert.NotEmpty(t, StrippedAgentPrompt)
	assert.Contains(t, StrippedAgentPrompt, "Providence Core")
}

func TestRunnerConcurrent5Agents(t *testing.T) {
	r := NewRunner()
	ids := make([]string, 5)
	for i := 0; i < 5; i++ {
		input := TaskInput{
			Description: fmt.Sprintf("concurrent-%d", i),
			Prompt:      "go",
			RunInBG:     true,
		}
		id, err := r.Spawn(context.Background(), input, DefaultAgentType(), mockExecutor(fmt.Sprintf("result-%d", i), nil, 20*time.Millisecond))
		require.NoError(t, err)
		ids[i] = id
	}

	// All 5 should complete successfully.
	for i, id := range ids {
		result := r.WaitFor(id)
		require.NotNil(t, result, "agent %d should have result", i)
		assert.Equal(t, "completed", result.Status)
		assert.Equal(t, fmt.Sprintf("result-%d", i), result.Result)
	}
}

func TestRunnerKillRunning(t *testing.T) {
	r := NewRunner()
	input := TaskInput{
		Description: "killable",
		Prompt:      "long running",
		RunInBG:     true,
	}

	id, err := r.Spawn(context.Background(), input, DefaultAgentType(), mockExecutor("", nil, 10*time.Second))
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond) // let goroutine start

	err = r.Kill(id)
	require.NoError(t, err)

	agent, ok := r.Get(id)
	require.True(t, ok)
	assert.Equal(t, "killed", agent.Status)

	result := r.WaitFor(id)
	require.NotNil(t, result)
	assert.Equal(t, "killed", result.Status)
}

func TestRunnerListAll(t *testing.T) {
	r := NewRunner()
	for i := 0; i < 3; i++ {
		input := TaskInput{Description: fmt.Sprintf("list-%d", i), Prompt: "go"}
		_, err := r.Spawn(context.Background(), input, DefaultAgentType(), mockExecutor("ok", nil, 0))
		require.NoError(t, err)
	}

	agents := r.List()
	assert.Len(t, agents, 3)

	// All should be completed (sync spawn).
	for _, a := range agents {
		assert.Equal(t, "completed", a.Status)
	}
}

func TestRunnerWaitForCompleted(t *testing.T) {
	r := NewRunner()
	input := TaskInput{Description: "already done", Prompt: "fast"}
	id, err := r.Spawn(context.Background(), input, DefaultAgentType(), mockExecutor("instant", nil, 0))
	require.NoError(t, err)

	// Agent already completed (sync). WaitFor should return immediately.
	start := time.Now()
	result := r.WaitFor(id)
	elapsed := time.Since(start)

	require.NotNil(t, result)
	assert.Equal(t, "completed", result.Status)
	assert.Equal(t, "instant", result.Result)
	assert.Less(t, elapsed, 50*time.Millisecond, "WaitFor on completed agent should return near-instantly")
}
