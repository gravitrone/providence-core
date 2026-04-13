package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gravitrone/providence-core/internal/engine/subagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSendMessageToolBasic(t *testing.T) {
	runner := subagent.NewRunner()
	tool := NewSendMessageTool(runner)

	assert.Equal(t, "SendMessage", tool.Name())
	assert.NotEmpty(t, tool.Description())
	assert.True(t, tool.ReadOnly())
}

func TestSendMessageToolSchema(t *testing.T) {
	runner := subagent.NewRunner()
	tool := NewSendMessageTool(runner)
	schema := tool.InputSchema()
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, props, "to")
	assert.Contains(t, props, "message")
	assert.Contains(t, props, "type")
}

func TestSendMessageToolMissing(t *testing.T) {
	runner := subagent.NewRunner()
	tool := NewSendMessageTool(runner)

	result := tool.Execute(context.Background(), map[string]any{})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "required")
}

func TestSendMessageToolNotFound(t *testing.T) {
	runner := subagent.NewRunner()
	tool := NewSendMessageTool(runner)

	result := tool.Execute(context.Background(), map[string]any{
		"to":      "agent-nonexistent",
		"message": "hello",
	})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "not found")
}

func TestSendMessageToolBroadcast(t *testing.T) {
	runner := subagent.NewRunner()
	tool := NewSendMessageTool(runner)

	// Spawn 2 async agents that block until message received.
	executor := func(ctx context.Context, _ string, _ subagent.AgentType) (string, error) {
		select {
		case <-time.After(5 * time.Second):
			return "timeout", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	for i := 0; i < 2; i++ {
		input := subagent.TaskInput{
			Description: "test",
			Prompt:      "wait",
			RunInBG:     true,
		}
		_, err := runner.Spawn(context.Background(), input, subagent.DefaultAgentType(), executor)
		require.NoError(t, err)
	}

	time.Sleep(10 * time.Millisecond) // let goroutines start

	// Broadcast.
	result := tool.Execute(context.Background(), map[string]any{
		"to":      "*",
		"message": "hello all",
	})

	require.False(t, result.IsError, result.Content)

	var resp map[string]any
	err := json.Unmarshal([]byte(result.Content), &resp)
	require.NoError(t, err)
	assert.Equal(t, "broadcast_complete", resp["status"])
	assert.Equal(t, float64(2), resp["delivered"])

	// Kill all to prevent goroutine leaks.
	runner.Close()
}

func TestSendMessageToolShutdownRequest(t *testing.T) {
	runner := subagent.NewRunner()
	tool := NewSendMessageTool(runner)

	// Spawn an agent that reads from inbox.
	received := make(chan string, 1)
	executor := func(ctx context.Context, _ string, _ subagent.AgentType) (string, error) {
		select {
		case <-time.After(5 * time.Second):
			return "timeout", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	_ = executor // we just need a running agent for the inbox

	input := subagent.TaskInput{
		Description: "receiver",
		Prompt:      "wait",
		Name:        "test-receiver",
		RunInBG:     true,
	}
	agentID, err := runner.Spawn(context.Background(), input, subagent.DefaultAgentType(), executor)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	// Send shutdown request by name.
	result := tool.Execute(context.Background(), map[string]any{
		"to":      agentID,
		"message": "please stop",
		"type":    "shutdown_request",
	})

	require.False(t, result.IsError, result.Content)

	// Read the message from the agent's inbox.
	agent, ok := runner.Get(agentID)
	require.True(t, ok)
	select {
	case msg := <-agent.Inbox:
		assert.Contains(t, msg, "[SHUTDOWN_REQUEST]")
		assert.Contains(t, msg, "please stop")
		received <- msg
	case <-time.After(1 * time.Second):
		t.Fatal("message not received in agent inbox")
	}

	runner.Close()
}

func TestSendMessageToolCompletedAgent(t *testing.T) {
	runner := subagent.NewRunner()
	tool := NewSendMessageTool(runner)

	// Spawn a sync agent that completes immediately.
	executor := func(_ context.Context, _ string, _ subagent.AgentType) (string, error) {
		return "done", nil
	}
	input := subagent.TaskInput{
		Description: "completes",
		Prompt:      "do",
		Name:        "completed-agent",
	}
	_, err := runner.Spawn(context.Background(), input, subagent.DefaultAgentType(), executor)
	require.NoError(t, err)

	// Try sending to the completed agent by name.
	result := tool.Execute(context.Background(), map[string]any{
		"to":      "completed-agent",
		"message": "continue",
	})

	// Should get auto-resume signal, not an error.
	require.False(t, result.IsError, result.Content)

	var resp map[string]any
	err = json.Unmarshal([]byte(result.Content), &resp)
	require.NoError(t, err)
	assert.Equal(t, "agent_completed", resp["status"])
	assert.Equal(t, true, resp["auto_resume"])
}
