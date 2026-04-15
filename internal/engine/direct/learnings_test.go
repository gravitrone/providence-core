package direct

import (
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractSessionLearningsEmptyHistoryYieldsZeroCounts verifies the
// extractor handles a fresh engine with no messages without crashing and
// reports zero tool calls and zero turns. The duration field still
// populates so callers can differentiate "no history" from "not invoked".
func TestExtractSessionLearningsEmptyHistoryYieldsZeroCounts(t *testing.T) {
	t.Parallel()

	e := &DirectEngine{history: NewConversationHistory(), sessionID: "sess-empty"}
	l := e.extractSessionLearnings(time.Now().Add(-500 * time.Millisecond))

	assert.Equal(t, "sess-empty", l.SessionID)
	assert.Empty(t, l.ToolCalls)
	assert.Zero(t, l.TurnCount)
	assert.NotEmpty(t, l.Duration, "Duration must always populate so the record is self-describing")
}

// TestExtractSessionLearningsToolTargetPrecedence pins the extraction
// order for the Target field. The production code walks a fixed key list
// (file_path, path, pattern, command, query) and takes the FIRST non-empty
// string match. Downstream analytics rely on this ordering; a future
// reorder or field rename would silently change bucket semantics if this
// test did not lock it down.
func TestExtractSessionLearningsToolTargetPrecedence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"file_path wins over path", map[string]any{"file_path": "first", "path": "second"}, "first"},
		{"path wins when no file_path", map[string]any{"path": "pp", "pattern": "pat"}, "pp"},
		{"pattern wins when no file_path/path", map[string]any{"pattern": "*.go"}, "*.go"},
		{"command wins when only command", map[string]any{"command": "ls"}, "ls"},
		{"query fallback", map[string]any{"query": "q"}, "q"},
		{"no known key yields empty Target", map[string]any{"other": "x"}, ""},
		{"empty string still falls through to next key", map[string]any{"file_path": "", "path": "pp"}, "pp"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &DirectEngine{history: NewConversationHistory(), sessionID: "sess-precedence"}
			e.history.mu.Lock()
			e.history.messages = append(e.history.messages, anthropic.NewAssistantMessage(
				anthropic.NewToolUseBlock("t1", tc.input, "TestTool"),
			))
			e.history.mu.Unlock()

			l := e.extractSessionLearnings(time.Now())
			require.Len(t, l.ToolCalls, 1)
			assert.Equal(t, "TestTool", l.ToolCalls[0].Name)
			assert.Equal(t, tc.want, l.ToolCalls[0].Target)
		})
	}
}

// TestExtractSessionLearningsCountsEachToolUseBlock verifies that every
// tool_use block in history produces exactly one entry in ToolCallLog and
// that TurnCount mirrors the total. Ordering matches history traversal
// order (important for callers doing timeline reconstruction).
func TestExtractSessionLearningsCountsEachToolUseBlock(t *testing.T) {
	t.Parallel()

	e := &DirectEngine{history: NewConversationHistory(), sessionID: "sess-count"}
	e.history.mu.Lock()
	e.history.messages = append(e.history.messages, anthropic.NewAssistantMessage(
		anthropic.NewToolUseBlock("t1", map[string]any{"file_path": "/a.go"}, "Read"),
		anthropic.NewToolUseBlock("t2", map[string]any{"pattern": "*.md"}, "Glob"),
		anthropic.NewToolUseBlock("t3", map[string]any{"command": "make test"}, "Bash"),
	))
	e.history.mu.Unlock()

	l := e.extractSessionLearnings(time.Now())

	require.Len(t, l.ToolCalls, 3)
	assert.Equal(t, 3, l.TurnCount)
	assert.Equal(t, "Read", l.ToolCalls[0].Name)
	assert.Equal(t, "/a.go", l.ToolCalls[0].Target)
	assert.Equal(t, "Glob", l.ToolCalls[1].Name)
	assert.Equal(t, "*.md", l.ToolCalls[1].Target)
	assert.Equal(t, "Bash", l.ToolCalls[2].Name)
	assert.Equal(t, "make test", l.ToolCalls[2].Target)
}

// TestSaveSessionLearningsNilStoreIsNoOp verifies the silent-fail
// contract: when no store is wired (e.g. tests, minimal embeds) the
// shutdown path must not panic or propagate an error. Losing the
// learnings record is acceptable; crashing the engine on shutdown is not.
func TestSaveSessionLearningsNilStoreIsNoOp(t *testing.T) {
	t.Parallel()

	e := &DirectEngine{history: NewConversationHistory(), sessionID: "sess-nil"}
	require.NotPanics(t, func() {
		e.saveSessionLearnings(nil, time.Now().Add(-1*time.Second))
	})
}

// fakeLearningsStore implements the storeIface minimal surface used by
// saveSessionLearnings. Captures the arguments for assertions.
type fakeLearningsStore struct {
	gotID      string
	gotPayload string
	err        error
}

func (f *fakeLearningsStore) SaveSessionLearnings(id, payload string) error {
	f.gotID = id
	f.gotPayload = payload
	return f.err
}

// TestSaveSessionLearningsPersistsJSONPayload verifies the happy path:
// with a non-nil store, saveSessionLearnings marshals the extracted
// learnings and hands the JSON to the store keyed by session ID.
func TestSaveSessionLearningsPersistsJSONPayload(t *testing.T) {
	t.Parallel()

	e := &DirectEngine{history: NewConversationHistory(), sessionID: "sess-persist"}
	e.history.mu.Lock()
	e.history.messages = append(e.history.messages, anthropic.NewAssistantMessage(
		anthropic.NewToolUseBlock("t1", map[string]any{"file_path": "/x.go"}, "Read"),
	))
	e.history.mu.Unlock()

	store := &fakeLearningsStore{}
	e.saveSessionLearnings(store, time.Now().Add(-2*time.Second))

	assert.Equal(t, "sess-persist", store.gotID, "store key must match session ID")
	assert.Contains(t, store.gotPayload, `"session_id":"sess-persist"`, "payload must be JSON with session_id")
	assert.Contains(t, store.gotPayload, `"Read"`, "payload must include tool call name")
	assert.Contains(t, store.gotPayload, `"/x.go"`, "payload must include tool call target")
}
