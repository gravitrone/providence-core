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
	assert.Equal(t, 100, re.InputTokens)
	assert.Equal(t, 50, re.OutputTokens)
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

// TestParseItemCompletedCommandExecutionOutputFormat pins the exact
// template the parser produces for command_execution outputs:
// "$ <cmd>\n(exit <N>)\n<agg>". Any silent reformatting would break
// the tool-result renderer downstream that splits on these markers.
func TestParseItemCompletedCommandExecutionOutputFormat(t *testing.T) {
	t.Parallel()

	line := `{"type":"item.completed","item":{"type":"command_execution","command":"ls","exit_code":0,"aggregated_output":"file.go\n"}}`
	pe, err := parseCodexEvent([]byte(line))
	require.NoError(t, err)

	tr, ok := pe.Data.(*engine.ToolResultEvent)
	require.True(t, ok)
	assert.Equal(t, "$ ls\n(exit 0)\nfile.go\n", tr.Output,
		"Output must follow '$ <cmd>\\n(exit <N>)\\n<agg>' exactly")
}

// TestParseItemCompletedCommandExecutionExitCodeDrivesIsError verifies
// the exit-code-to-IsError mapping. Zero is success, non-zero is error;
// a nil exit_code defaults to zero success. Downstream rendering picks
// the error styling off IsError, not off the output text.
func TestParseItemCompletedCommandExecutionExitCodeDrivesIsError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		line  string
		isErr bool
	}{
		{"exit 0 success", `{"type":"item.completed","item":{"type":"command_execution","command":"true","exit_code":0,"aggregated_output":""}}`, false},
		{"exit 1 error", `{"type":"item.completed","item":{"type":"command_execution","command":"false","exit_code":1,"aggregated_output":""}}`, true},
		{"exit 127 error", `{"type":"item.completed","item":{"type":"command_execution","command":"nope","exit_code":127,"aggregated_output":"not found"}}`, true},
		{"missing exit_code defaults to 0 success", `{"type":"item.completed","item":{"type":"command_execution","command":"pending","aggregated_output":""}}`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pe, err := parseCodexEvent([]byte(tc.line))
			require.NoError(t, err)
			tr, ok := pe.Data.(*engine.ToolResultEvent)
			require.True(t, ok)
			assert.Equal(t, tc.isErr, tr.IsError)
		})
	}
}

// TestParseItemCompletedFileChangeRawJSONPreserved verifies that the
// polymorphic Changes field (json.RawMessage) passes through untouched
// as the tool_result Output. A naive re-marshal would strip whitespace
// or reorder keys; downstream diff renderers rely on byte-identical
// content.
func TestParseItemCompletedFileChangeRawJSONPreserved(t *testing.T) {
	t.Parallel()

	// Nested structure with deliberate formatting quirks (nested keys
	// in non-alphabetical order) to catch re-marshal regressions.
	raw := `[{"type":"edit","path":"a.go","old_lines":3,"new_lines":5}]`
	line := `{"type":"item.completed","item":{"type":"file_change","changes":` + raw + `}}`

	pe, err := parseCodexEvent([]byte(line))
	require.NoError(t, err)

	tr, ok := pe.Data.(*engine.ToolResultEvent)
	require.True(t, ok)
	assert.Equal(t, "file_change", tr.ToolName)
	assert.Equal(t, raw, tr.Output, "Changes raw bytes must survive the parse unmodified")
}

// TestParseItemCompletedUnknownItemTypeYieldsZeroEvent verifies the
// default branch: item types that Providence does not model yet (e.g.
// "thread.started") return a zero ParsedEvent without error so the
// stream consumer simply skips the line.
func TestParseItemCompletedUnknownItemTypeYieldsZeroEvent(t *testing.T) {
	t.Parallel()

	line := `{"type":"item.completed","item":{"type":"thread.started"}}`
	pe, err := parseCodexEvent([]byte(line))
	require.NoError(t, err)
	assert.Equal(t, "", pe.Type, "unknown item type must produce a zero ParsedEvent")
	assert.Nil(t, pe.Data)
}

// TestParseErrorEmptyMessageFallsBackToEventType verifies the fallback
// at protocol.go:160-163: an error event whose Message is empty uses
// the event type string as the Result payload so UI surfaces can still
// render something meaningful ("turn.failed") instead of an empty line.
func TestParseErrorEmptyMessageFallsBackToEventType(t *testing.T) {
	t.Parallel()

	line := `{"type":"turn.failed","message":""}`
	pe, err := parseCodexEvent([]byte(line))
	require.NoError(t, err)

	re, ok := pe.Data.(*engine.ResultEvent)
	require.True(t, ok)
	assert.Equal(t, "error", re.Subtype)
	assert.True(t, re.IsError)
	assert.Equal(t, "turn.failed", re.Result, "empty message must fall back to the event type string")
}

// TestParseCodexEventMalformedItemJSONReturnsError feeds a syntactically
// valid outer envelope with a broken inner item and asserts the parser
// surfaces a wrapped "parse item.completed" error rather than silently
// swallowing the line. A silent-swallow regression would hide stream
// corruption.
func TestParseCodexEventMalformedItemJSONReturnsError(t *testing.T) {
	t.Parallel()

	// item is an array where the parser expects an object.
	line := `{"type":"item.completed","item":[1,2,3]}`
	_, err := parseCodexEvent([]byte(line))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse item.completed",
		"malformed inner item must surface as a wrapped item.completed parse error")
}
