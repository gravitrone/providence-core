package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngineFactoryRegistration(t *testing.T) {
	// Register mock factories to verify the registration mechanism works.
	mockFactory := func(cfg EngineConfig) (Engine, error) {
		return &mockEngine{status: StatusIdle}, nil
	}
	testTypes := []EngineType{"test_claude", "test_direct", "test_codex_re", "test_opencode"}
	for _, et := range testTypes {
		RegisterFactory(et, mockFactory)
	}
	for _, et := range testTypes {
		t.Run(string(et), func(t *testing.T) {
			_, ok := factories[et]
			assert.True(t, ok, "engine type %q should be registered", et)
		})
	}
}

func TestNewEngineRegistered(t *testing.T) {
	RegisterFactory("test_mock", func(cfg EngineConfig) (Engine, error) {
		return &mockEngine{status: StatusIdle}, nil
	})
	eng, err := NewEngine(EngineConfig{Type: "test_mock"})
	require.NoError(t, err)
	assert.NotNil(t, eng)
	assert.Equal(t, StatusIdle, eng.Status())
}

func TestNewEngineFactoryError(t *testing.T) {
	RegisterFactory("test_fail", func(cfg EngineConfig) (Engine, error) {
		return nil, fmt.Errorf("init failed")
	})
	eng, err := NewEngine(EngineConfig{Type: "test_fail"})
	assert.Error(t, err)
	assert.Nil(t, eng)
	assert.Contains(t, err.Error(), "init failed")
}

func TestNewEngineInvalidType(t *testing.T) {
	cfg := EngineConfig{Type: "nonexistent_engine_xyz"}
	eng, err := NewEngine(cfg)
	require.Error(t, err)
	assert.Nil(t, eng)
	assert.Contains(t, err.Error(), "unknown engine type")
}

func TestConversationStateJSON(t *testing.T) {
	state := &ConversationState{
		Messages: []PortableMessage{
			{Role: "user", Content: "hello", UUID: "u1"},
			{Role: "assistant", Content: "hi", UUID: "u2"},
		},
		SystemPrompt: "test prompt",
		Model:        "claude-sonnet-4-6",
		Engine:       "direct",
		SessionID:    "sess-abc",
		TokenCount:   2000,
		FileState:    map[string]int64{"/tmp/a.go": 999},
	}

	data, err := MarshalState(state)
	require.NoError(t, err)

	restored, err := UnmarshalState(data)
	require.NoError(t, err)

	assert.Equal(t, state.SystemPrompt, restored.SystemPrompt)
	assert.Equal(t, state.Model, restored.Model)
	assert.Equal(t, state.Engine, restored.Engine)
	assert.Equal(t, state.SessionID, restored.SessionID)
	assert.Equal(t, state.TokenCount, restored.TokenCount)
	assert.Equal(t, state.FileState, restored.FileState)
	require.Len(t, restored.Messages, 2)
	assert.Equal(t, "user", restored.Messages[0].Role)
	assert.Equal(t, "hello", restored.Messages[0].Content)
	assert.Equal(t, "u1", restored.Messages[0].UUID)
}

func TestPortableMessageJSON(t *testing.T) {
	msg := PortableMessage{
		Role:       "tool",
		Content:    "file read ok",
		ToolCallID: "tc_42",
		ToolName:   "Read",
		ToolInput:  `{"path":"/x"}`,
		UUID:       "msg-7",
	}

	data, err := MarshalState(&ConversationState{
		Messages: []PortableMessage{msg},
	})
	require.NoError(t, err)

	restored, err := UnmarshalState(data)
	require.NoError(t, err)
	require.Len(t, restored.Messages, 1)

	got := restored.Messages[0]
	assert.Equal(t, msg.Role, got.Role)
	assert.Equal(t, msg.Content, got.Content)
	assert.Equal(t, msg.ToolCallID, got.ToolCallID)
	assert.Equal(t, msg.ToolName, got.ToolName)
	assert.Equal(t, msg.ToolInput, got.ToolInput)
	assert.Equal(t, msg.UUID, got.UUID)
}

func TestSerializeRestoreState_MockRoundtrip(t *testing.T) {
	eng := &mockEngine{status: StatusIdle}
	messages := []RestoredMessage{
		{Role: "user", Content: "question"},
		{Role: "assistant", Content: "answer"},
		{Role: "tool", Content: "output", ToolCallID: "tc1", ToolName: "Bash", ToolInput: `{"cmd":"ls"}`},
	}

	state, err := SerializeState(eng, messages, "sys prompt", "model-x", "direct")
	require.NoError(t, err)

	// Marshal -> unmarshal to simulate persistence.
	data, err := MarshalState(state)
	require.NoError(t, err)
	state2, err := UnmarshalState(data)
	require.NoError(t, err)

	target := &mockEngine{status: StatusIdle}
	err = RestoreState(target, state2)
	require.NoError(t, err)

	require.Len(t, target.restored, 3)
	assert.Equal(t, "user", target.restored[0].Role)
	assert.Equal(t, "question", target.restored[0].Content)
	assert.Equal(t, "tool", target.restored[2].Role)
	assert.Equal(t, "tc1", target.restored[2].ToolCallID)
	assert.Equal(t, "Bash", target.restored[2].ToolName)
}
