package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gravitrone/providence-core/internal/engine/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEngine implements Engine for portability tests.
type mockEngine struct {
	status   SessionStatus
	restored []RestoredMessage
}

func (m *mockEngine) Send(string) error                         { return nil }
func (m *mockEngine) Events() <-chan ParsedEvent                { return make(chan ParsedEvent) }
func (m *mockEngine) RespondPermission(string, string) error    { return nil }
func (m *mockEngine) Interrupt()                                {}
func (m *mockEngine) Cancel()                                   {}
func (m *mockEngine) Close()                                    {}
func (m *mockEngine) Status() SessionStatus                     { return m.status }
func (m *mockEngine) TriggerCompact(_ context.Context) error    { return nil }
func (m *mockEngine) SessionBus() *session.Bus                  { return session.NewBus() }
func (m *mockEngine) RestoreHistory(msgs []RestoredMessage) error {
	m.restored = msgs
	return nil
}

func TestSerializeState(t *testing.T) {
	e := &mockEngine{status: StatusIdle}
	messages := []RestoredMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
		{Role: "tool", Content: "file contents", ToolCallID: "tc_1", ToolName: "Read", ToolInput: `{"path": "/tmp/x"}`},
	}

	state, err := SerializeState(e, messages, "you are helpful", "gpt-5.4", "codex_re")
	require.NoError(t, err)

	assert.Equal(t, "you are helpful", state.SystemPrompt)
	assert.Equal(t, "gpt-5.4", state.Model)
	assert.Equal(t, "codex_re", state.Engine)
	assert.NotEmpty(t, state.SessionID)
	assert.Len(t, state.Messages, 3)
	assert.Equal(t, "user", state.Messages[0].Role)
	assert.Equal(t, "hello", state.Messages[0].Content)
	assert.NotEmpty(t, state.Messages[0].UUID)
	assert.Equal(t, "tc_1", state.Messages[2].ToolCallID)
	assert.Equal(t, "Read", state.Messages[2].ToolName)
}

func TestSerializeState_NilEngine(t *testing.T) {
	_, err := SerializeState(nil, nil, "", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil engine")
}

func TestRestoreState(t *testing.T) {
	e := &mockEngine{status: StatusIdle}
	state := &ConversationState{
		Messages: []PortableMessage{
			{Role: "user", Content: "what is 2+2", UUID: "u1"},
			{Role: "assistant", Content: "4", UUID: "u2"},
		},
		SystemPrompt: "math helper",
		Model:        "claude-sonnet-4-6",
		Engine:       "direct",
	}

	err := RestoreState(e, state)
	require.NoError(t, err)
	require.Len(t, e.restored, 2)
	assert.Equal(t, "user", e.restored[0].Role)
	assert.Equal(t, "what is 2+2", e.restored[0].Content)
	assert.Equal(t, "assistant", e.restored[1].Role)
	assert.Equal(t, "4", e.restored[1].Content)
}

func TestRestoreState_NilEngine(t *testing.T) {
	state := &ConversationState{}
	err := RestoreState(nil, state)
	assert.Error(t, err)
}

func TestRestoreState_NilState(t *testing.T) {
	e := &mockEngine{}
	err := RestoreState(e, nil)
	assert.Error(t, err)
}

func TestPortableMessageRoundtrip(t *testing.T) {
	original := []PortableMessage{
		{Role: "user", Content: "build me a thing", UUID: "msg-1"},
		{Role: "assistant", Content: "sure", UUID: "msg-2"},
		{Role: "tool", Content: "done", ToolCallID: "tc_1", ToolName: "Bash", ToolInput: `{"command":"ls"}`, UUID: "msg-3"},
	}

	state := &ConversationState{
		Messages:     original,
		SystemPrompt: "test prompt",
		Model:        "gpt-5.4",
		Engine:       "codex_re",
		SessionID:    "sess-1",
		TokenCount:   1500,
		FileState:    map[string]int64{"/tmp/a.go": 1234567890},
	}

	data, err := json.Marshal(state)
	require.NoError(t, err)

	var restored ConversationState
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, state.SystemPrompt, restored.SystemPrompt)
	assert.Equal(t, state.Model, restored.Model)
	assert.Equal(t, state.Engine, restored.Engine)
	assert.Equal(t, state.SessionID, restored.SessionID)
	assert.Equal(t, state.TokenCount, restored.TokenCount)
	assert.Equal(t, state.FileState, restored.FileState)
	require.Len(t, restored.Messages, 3)

	for i, msg := range original {
		assert.Equal(t, msg.Role, restored.Messages[i].Role)
		assert.Equal(t, msg.Content, restored.Messages[i].Content)
		assert.Equal(t, msg.ToolCallID, restored.Messages[i].ToolCallID)
		assert.Equal(t, msg.ToolName, restored.Messages[i].ToolName)
		assert.Equal(t, msg.ToolInput, restored.Messages[i].ToolInput)
		assert.Equal(t, msg.UUID, restored.Messages[i].UUID)
	}
}

func TestMarshalUnmarshalState(t *testing.T) {
	state := &ConversationState{
		Messages: []PortableMessage{
			{Role: "user", Content: "test", UUID: "u1"},
		},
		SystemPrompt: "prompt",
		Model:        "model",
		Engine:       "direct",
		SessionID:    "s1",
		TokenCount:   500,
		FileState:    map[string]int64{},
	}

	data, err := MarshalState(state)
	require.NoError(t, err)

	restored, err := UnmarshalState(data)
	require.NoError(t, err)
	assert.Equal(t, state.SystemPrompt, restored.SystemPrompt)
	assert.Equal(t, state.Model, restored.Model)
	assert.Len(t, restored.Messages, 1)
}

func TestUnmarshalState_InvalidJSON(t *testing.T) {
	_, err := UnmarshalState([]byte("not json"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal conversation state")
}
