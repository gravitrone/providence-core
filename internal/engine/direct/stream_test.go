package direct

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTranslateStreamEvent_TextDelta(t *testing.T) {
	// Build a ContentBlockDeltaEvent via JSON round-trip since the SDK uses union types.
	raw := `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`
	var event anthropic.MessageStreamEventUnion
	err := json.Unmarshal([]byte(raw), &event)
	require.NoError(t, err)

	pe := translateStreamEvent(event)
	require.NotNil(t, pe)
	assert.Equal(t, "stream_event", pe.Type)

	se, ok := pe.Data.(*engine.StreamEvent)
	require.True(t, ok)
	assert.Equal(t, "content_block_delta", se.Event.Type)
	assert.Equal(t, 0, se.Event.Index)
	require.NotNil(t, se.Event.Delta)
	assert.Equal(t, "text_delta", se.Event.Delta.Type)
	assert.Equal(t, "hello", se.Event.Delta.Text)
}

func TestTranslateStreamEvent_MessageStart_Skipped(t *testing.T) {
	raw := `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":0}}}`
	var event anthropic.MessageStreamEventUnion
	err := json.Unmarshal([]byte(raw), &event)
	require.NoError(t, err)

	pe := translateStreamEvent(event)
	assert.Nil(t, pe, "message_start should be skipped")
}

func TestExtractToolCalls(t *testing.T) {
	raw := `{
		"id": "msg_1",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Let me check."},
			{"type": "tool_use", "id": "tu_1", "name": "read_file", "input": {"path": "/tmp/x"}}
		],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "tool_use",
		"stop_sequence": null,
		"usage": {"input_tokens": 10, "output_tokens": 20}
	}`
	var msg anthropic.Message
	err := json.Unmarshal([]byte(raw), &msg)
	require.NoError(t, err)

	calls := extractToolCalls(msg)
	require.Len(t, calls, 1)
	assert.Equal(t, "tu_1", calls[0].ID)
	assert.Equal(t, "read_file", calls[0].Name)
	assert.Equal(t, "/tmp/x", calls[0].Input["path"])
}

func TestExtractToolCalls_NoToolUse(t *testing.T) {
	raw := `{
		"id": "msg_1",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Just text."}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"stop_sequence": null,
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`
	var msg anthropic.Message
	err := json.Unmarshal([]byte(raw), &msg)
	require.NoError(t, err)

	calls := extractToolCalls(msg)
	assert.Empty(t, calls)
}
