package claude

import (
	"testing"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEvent_SystemInit(t *testing.T) {
	line := []byte(`{"type":"system","subtype":"init","session_id":"abc-123","tools":["WebSearch","WebFetch"],"model":"claude-sonnet-4-20250514"}`)

	eventType, data, err := ParseEvent(line)
	require.NoError(t, err)
	assert.Equal(t, "system", eventType)

	e, ok := data.(*engine.SystemInitEvent)
	require.True(t, ok)
	assert.Equal(t, "init", e.Subtype)
	assert.Equal(t, "abc-123", e.SessionID)
	assert.Equal(t, "claude-sonnet-4-20250514", e.Model)
	assert.Equal(t, []string{"WebSearch", "WebFetch"}, e.Tools)
}

func TestParseEvent_StreamEvent(t *testing.T) {
	line := []byte(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}}`)

	eventType, data, err := ParseEvent(line)
	require.NoError(t, err)
	assert.Equal(t, "stream_event", eventType)

	e, ok := data.(*engine.StreamEvent)
	require.True(t, ok)
	assert.Equal(t, "content_block_delta", e.Event.Type)
	assert.Equal(t, 0, e.Event.Index)
	require.NotNil(t, e.Event.Delta)
	assert.Equal(t, "text_delta", e.Event.Delta.Type)
	assert.Equal(t, "Hello", e.Event.Delta.Text)
}

func TestParseEvent_AssistantEvent(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"Found 3 jobs."}]}}`)

	eventType, data, err := ParseEvent(line)
	require.NoError(t, err)
	assert.Equal(t, "assistant", eventType)

	e, ok := data.(*engine.AssistantEvent)
	require.True(t, ok)
	require.Len(t, e.Message.Content, 1)
	assert.Equal(t, "text", e.Message.Content[0].Type)
	assert.Equal(t, "Found 3 jobs.", e.Message.Content[0].Text)
}

func TestParseEvent_ResultSuccess(t *testing.T) {
	line := []byte(`{"type":"result","subtype":"success","result":"done","session_id":"abc-123","total_cost_usd":0.003,"is_error":false}`)

	eventType, data, err := ParseEvent(line)
	require.NoError(t, err)
	assert.Equal(t, "result", eventType)

	e, ok := data.(*engine.ResultEvent)
	require.True(t, ok)
	assert.Equal(t, "success", e.Subtype)
	assert.Equal(t, "abc-123", e.SessionID)
	assert.InDelta(t, 0.003, e.TotalCostUSD, 1e-9)
	assert.False(t, e.IsError)
}

func TestParseEvent_ResultError(t *testing.T) {
	line := []byte(`{"type":"result","subtype":"error","result":"rate limit exceeded","session_id":"abc-123","total_cost_usd":0,"is_error":true}`)

	eventType, data, err := ParseEvent(line)
	require.NoError(t, err)
	assert.Equal(t, "result", eventType)

	e, ok := data.(*engine.ResultEvent)
	require.True(t, ok)
	assert.Equal(t, "error", e.Subtype)
	assert.True(t, e.IsError)
}

func TestParseEvent_PermissionRequest(t *testing.T) {
	line := []byte(`{"type":"permission_request","tool":{"name":"WebFetch","input":{"url":"https://jobs.example.com"}},"question_id":"q-456","options":[{"id":"allow","label":"Allow"},{"id":"deny","label":"Deny"}]}`)

	eventType, data, err := ParseEvent(line)
	require.NoError(t, err)
	assert.Equal(t, "permission_request", eventType)

	e, ok := data.(*engine.PermissionRequestEvent)
	require.True(t, ok)
	assert.Equal(t, "WebFetch", e.Tool.Name)
	assert.Equal(t, "q-456", e.QuestionID)
	require.Len(t, e.Options, 2)
	assert.Equal(t, "allow", e.Options[0].ID)
	assert.Equal(t, "deny", e.Options[1].ID)
}

func TestParseEvent_UnknownType(t *testing.T) {
	line := []byte(`{"type":"rate_limit_event","message":"slow down"}`)

	eventType, data, err := ParseEvent(line)
	require.NoError(t, err)
	assert.Equal(t, "rate_limit_event", eventType)

	e, ok := data.(*engine.Event)
	require.True(t, ok)
	assert.Equal(t, "rate_limit_event", e.Type)
}

func TestParseEvent_InvalidJSON(t *testing.T) {
	line := []byte(`not json at all`)

	_, _, err := ParseEvent(line)
	assert.Error(t, err)
}
