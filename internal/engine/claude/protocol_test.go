package claude

import (
	"encoding/json"
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

// TestParseEventAssistantWithToolUseBlock verifies that ContentPart
// entries of type "tool_use" survive the assistant-event parse. The
// existing assistant test only exercises a plain text block, so a
// regression in the JSON tags on ContentPart.ID/Name/Input would slip
// past it silently. This test pins the tool-use shape end to end.
func TestParseEventAssistantWithToolUseBlock(t *testing.T) {
	t.Parallel()

	line := []byte(`{"type":"assistant","message":{"content":[` +
		`{"type":"text","text":"reading file"},` +
		`{"type":"tool_use","id":"tu_123","name":"Read","input":{"path":"/a.go"}}` +
		`]}}`)

	eventType, data, err := ParseEvent(line)
	require.NoError(t, err)
	assert.Equal(t, "assistant", eventType)

	e, ok := data.(*engine.AssistantEvent)
	require.True(t, ok)
	require.Len(t, e.Message.Content, 2)

	assert.Equal(t, "text", e.Message.Content[0].Type)
	assert.Equal(t, "reading file", e.Message.Content[0].Text)

	tu := e.Message.Content[1]
	assert.Equal(t, "tool_use", tu.Type)
	assert.Equal(t, "tu_123", tu.ID, "tool_use blocks must carry their id so the tool_result can pair with them later")
	assert.Equal(t, "Read", tu.Name, "tool name must survive the parse")
	require.NotNil(t, tu.Input, "tool_use input must not be dropped")

	input, ok := tu.Input.(map[string]any)
	require.True(t, ok, "tool_use input must decode as a generic map")
	assert.Equal(t, "/a.go", input["path"])
}

// TestParseEventSystemNonInitSubtypeRoutesToBase verifies the fallback
// branch: a system event whose subtype is not "init" must decode to
// the generic engine.Event rather than engine.SystemInitEvent. Callers
// rely on this to detect unknown system messages without blowing up
// their type assertions.
func TestParseEventSystemNonInitSubtypeRoutesToBase(t *testing.T) {
	t.Parallel()

	line := []byte(`{"type":"system","subtype":"notice","message":"hello"}`)

	eventType, data, err := ParseEvent(line)
	require.NoError(t, err)
	assert.Equal(t, "system", eventType)

	e, ok := data.(*engine.Event)
	require.True(t, ok, "non-init system subtype must route to *engine.Event, not *engine.SystemInitEvent")
	assert.Equal(t, "system", e.Type)
	assert.Equal(t, "notice", e.Subtype)
}

// TestUserMessageMarshalRoundTrip pins the NDJSON shape that claude's
// subprocess expects on stdin. Send() appends a newline to the marshal
// output; this test asserts the marshal body itself carries a top-level
// "type" and a nested "message" with role and content so the subprocess
// recognises the turn.
func TestUserMessageMarshalRoundTrip(t *testing.T) {
	t.Parallel()

	m := UserMessage{
		Type: "user",
		Message: engine.MessageBody{
			Role: "user",
			Content: []engine.ContentPart{
				{Type: "text", Text: "hello"},
			},
		},
	}

	data, err := json.Marshal(m)
	require.NoError(t, err)

	// Shape check: the subprocess consumes this as a single JSON line, so
	// no stray whitespace or unexpected fields.
	assert.Contains(t, string(data), `"type":"user"`)
	assert.Contains(t, string(data), `"role":"user"`)
	assert.Contains(t, string(data), `"text":"hello"`)

	// Round-trip: a fresh UserMessage decoded from the bytes equals the
	// original modulo the generic ContentPart.Input field which is nil
	// either way for a text-only turn.
	var decoded UserMessage
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, m.Type, decoded.Type)
	assert.Equal(t, m.Message.Role, decoded.Message.Role)
	require.Len(t, decoded.Message.Content, 1)
	assert.Equal(t, "text", decoded.Message.Content[0].Type)
	assert.Equal(t, "hello", decoded.Message.Content[0].Text)
}

// TestPermissionResponseMarshalShape pins the NDJSON shape the claude
// subprocess expects when the UI resolves a permission request. The
// three fields must land in lowercase snake_case keys; the subprocess
// silently ignores mis-keyed replies, so a regression here would show
// up as a hung permission prompt rather than a parse error.
func TestPermissionResponseMarshalShape(t *testing.T) {
	t.Parallel()

	r := PermissionResponse{
		Type:       "permission_response",
		QuestionID: "q-42",
		OptionID:   "allow",
	}

	data, err := json.Marshal(r)
	require.NoError(t, err)

	s := string(data)
	assert.Contains(t, s, `"type":"permission_response"`)
	assert.Contains(t, s, `"question_id":"q-42"`)
	assert.Contains(t, s, `"option_id":"allow"`)

	var decoded PermissionResponse
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, r, decoded, "PermissionResponse must round-trip losslessly")
}

// TestParseEventResultMissingCostDefaultsToZero verifies that result
// events arriving without a total_cost_usd field decode cleanly with
// TotalCostUSD at the Go zero value. The field was added mid-schema
// by upstream; any change that would silently fabricate a non-zero
// default would mislead cost-tracking downstream.
func TestParseEventResultMissingCostDefaultsToZero(t *testing.T) {
	t.Parallel()

	line := []byte(`{"type":"result","subtype":"success","result":"ok","session_id":"s-1","is_error":false}`)

	eventType, data, err := ParseEvent(line)
	require.NoError(t, err)
	assert.Equal(t, "result", eventType)

	e, ok := data.(*engine.ResultEvent)
	require.True(t, ok)
	assert.Equal(t, float64(0), e.TotalCostUSD, "missing total_cost_usd must decode as zero, not some sentinel")
	assert.Equal(t, "success", e.Subtype)
	assert.False(t, e.IsError)
}
