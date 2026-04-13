package codex_headless

import (
	"testing"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Event Mapping Tests ---

func TestParseAgentMessage(t *testing.T) {
	line := `{"type":"item.completed","item":{"type":"agent_message","text":"Hello world"}}`
	pe, err := parseCodexEvent([]byte(line))
	require.NoError(t, err)
	assert.Equal(t, "assistant", pe.Type)

	ae, ok := pe.Data.(*engine.AssistantEvent)
	require.True(t, ok)
	require.Len(t, ae.Message.Content, 1)
	assert.Equal(t, "text", ae.Message.Content[0].Type)
	assert.Equal(t, "Hello world", ae.Message.Content[0].Text)
}

func TestParseCommandExecution(t *testing.T) {
	line := `{"type":"item.completed","item":{"type":"command_execution","command":"ls -la","exit_code":0,"aggregated_output":"total 42\ndrwxr-xr-x 5 user user 160 Apr 13 12:00 ."}}`
	pe, err := parseCodexEvent([]byte(line))
	require.NoError(t, err)
	assert.Equal(t, "tool_result", pe.Type)

	tr, ok := pe.Data.(*engine.ToolResultEvent)
	require.True(t, ok)
	assert.Equal(t, "command_execution", tr.ToolName)
	assert.Contains(t, tr.Output, "$ ls -la")
	assert.Contains(t, tr.Output, "(exit 0)")
	assert.Contains(t, tr.Output, "total 42")
	assert.False(t, tr.IsError)
}

func TestParseCommandExecutionFailure(t *testing.T) {
	exit1 := 1
	_ = exit1 // used indirectly in JSON
	line := `{"type":"item.completed","item":{"type":"command_execution","command":"false","exit_code":1,"aggregated_output":""}}`
	pe, err := parseCodexEvent([]byte(line))
	require.NoError(t, err)
	assert.Equal(t, "tool_result", pe.Type)

	tr, ok := pe.Data.(*engine.ToolResultEvent)
	require.True(t, ok)
	assert.True(t, tr.IsError)
	assert.Contains(t, tr.Output, "(exit 1)")
}

func TestParseFileChange(t *testing.T) {
	line := `{"type":"item.completed","item":{"type":"file_change","changes":[{"file":"main.go","action":"modified"}]}}`
	pe, err := parseCodexEvent([]byte(line))
	require.NoError(t, err)
	assert.Equal(t, "tool_result", pe.Type)

	tr, ok := pe.Data.(*engine.ToolResultEvent)
	require.True(t, ok)
	assert.Equal(t, "file_change", tr.ToolName)
	assert.Contains(t, tr.Output, "main.go")
}

func TestParseTurnCompleted(t *testing.T) {
	line := `{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":50}}`
	pe, err := parseCodexEvent([]byte(line))
	require.NoError(t, err)
	assert.Equal(t, "result", pe.Type)

	re, ok := pe.Data.(*engine.ResultEvent)
	require.True(t, ok)
	assert.Equal(t, "success", re.Subtype)
	assert.False(t, re.IsError)
}

func TestParseError(t *testing.T) {
	line := `{"type":"error","message":"rate limit exceeded"}`
	pe, err := parseCodexEvent([]byte(line))
	require.NoError(t, err)
	assert.Equal(t, "result", pe.Type)

	re, ok := pe.Data.(*engine.ResultEvent)
	require.True(t, ok)
	assert.True(t, re.IsError)
	assert.Equal(t, "error", re.Subtype)
	assert.Equal(t, "rate limit exceeded", re.Result)
}

func TestParseTurnFailed(t *testing.T) {
	line := `{"type":"turn.failed","message":"context too long"}`
	pe, err := parseCodexEvent([]byte(line))
	require.NoError(t, err)
	assert.Equal(t, "result", pe.Type)

	re, ok := pe.Data.(*engine.ResultEvent)
	require.True(t, ok)
	assert.True(t, re.IsError)
	assert.Equal(t, "context too long", re.Result)
}

func TestParseIgnoredEvents(t *testing.T) {
	ignored := []string{
		`{"type":"thread.started","thread_id":"abc-123"}`,
		`{"type":"turn.started"}`,
	}
	for _, line := range ignored {
		pe, err := parseCodexEvent([]byte(line))
		require.NoError(t, err)
		assert.Empty(t, pe.Type, "expected empty type for ignored event: %s", line)
	}
}

func TestParseInvalidJSON(t *testing.T) {
	_, err := parseCodexEvent([]byte("not json"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse codex event base")
}

func TestParseUnknownItemType(t *testing.T) {
	line := `{"type":"item.completed","item":{"type":"unknown_item"}}`
	pe, err := parseCodexEvent([]byte(line))
	require.NoError(t, err)
	assert.Empty(t, pe.Type, "unknown item type should be silently skipped")
}

func TestParseErrorEmptyMessage(t *testing.T) {
	line := `{"type":"error","message":""}`
	pe, err := parseCodexEvent([]byte(line))
	require.NoError(t, err)

	re, ok := pe.Data.(*engine.ResultEvent)
	require.True(t, ok)
	assert.True(t, re.IsError)
	assert.Equal(t, "error", re.Result, "empty message should fall back to event type")
}

func TestParseCommandExecutionNilExitCode(t *testing.T) {
	// exit_code missing from JSON - should default to 0.
	line := `{"type":"item.completed","item":{"type":"command_execution","command":"echo hi","aggregated_output":"hi"}}`
	pe, err := parseCodexEvent([]byte(line))
	require.NoError(t, err)

	tr, ok := pe.Data.(*engine.ToolResultEvent)
	require.True(t, ok)
	assert.False(t, tr.IsError)
	assert.Contains(t, tr.Output, "(exit 0)")
}
