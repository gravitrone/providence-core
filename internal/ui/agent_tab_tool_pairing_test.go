package ui

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/store"
)

// TestResumePairsToolsByID guards the regression where two in-flight calls
// to the same tool collapsed onto the same synthetic id because the legacy
// restore path keyed by toolName only. With persisted tool_call_id events
// each row carries its own id, so the two Read invocations stay distinct
// after restore.
func TestResumePairsToolsByID(t *testing.T) {
	// Four rows: assistant-tool-use (Read #1), tool-result (Read #1),
	// assistant-tool-use (Read #2), tool-result (Read #2). The broken
	// legacy pairing would assign the same id to both use/result rows
	// because the map is keyed by tool name.
	rows := []store.MessageRow{
		{ID: 10, Role: "assistant", Content: "calling Read 1", ToolName: "Read", ToolArgs: `{"path":"a"}`, Done: true, CreatedAt: time.Unix(1, 0)},
		{ID: 11, Role: "tool", ToolName: "Read", ToolOutput: "contents A", Done: true, CreatedAt: time.Unix(2, 0)},
		{ID: 12, Role: "assistant", Content: "calling Read 2", ToolName: "Read", ToolArgs: `{"path":"b"}`, Done: true, CreatedAt: time.Unix(3, 0)},
		{ID: 13, Role: "tool", ToolName: "Read", ToolOutput: "contents B", Done: true, CreatedAt: time.Unix(4, 0)},
	}

	events := []store.MessageEvent{
		mustToolCallIDEvent(t, 1, 10, "Read", "call_Read_one"),
		mustToolCallIDEvent(t, 2, 11, "Read", "call_Read_one"),
		mustToolCallIDEvent(t, 3, 12, "Read", "call_Read_two"),
		mustToolCallIDEvent(t, 4, 13, "Read", "call_Read_two"),
	}

	callIDMap := toolCallIDMapFromEvents(events)
	require.Len(t, callIDMap, 4)
	assert.Equal(t, "call_Read_one", callIDMap[10])
	assert.Equal(t, "call_Read_one", callIDMap[11])
	assert.Equal(t, "call_Read_two", callIDMap[12])
	assert.Equal(t, "call_Read_two", callIDMap[13])

	restored, pending := replayRestoreForTest(rows, callIDMap)
	require.Len(t, restored, 4)
	assert.Equal(t, "call_Read_one", restored[1].ToolCallID, "tool result #1 keeps the id minted for use #1")
	assert.Equal(t, "call_Read_two", restored[3].ToolCallID, "tool result #2 keeps the id minted for use #2")
	assert.NotEqual(t, restored[1].ToolCallID, restored[3].ToolCallID, "two in-flight reads must keep distinct ids")
	assert.Empty(t, pending["Read"], "every use had a matching result, the pending queue drains")
}

// TestResumePairsToolsByIDFallsBackOnMissingEvents verifies that sessions
// written before the event log existed still restore. The pairing falls back
// to FIFO-pop of the pending queue and finally to a fresh synthetic id.
func TestResumePairsToolsByIDFallsBackOnMissingEvents(t *testing.T) {
	rows := []store.MessageRow{
		{ID: 1, Role: "assistant", Content: "calling Write", ToolName: "Write", ToolArgs: `{"path":"x"}`, Done: true},
		{ID: 2, Role: "tool", ToolName: "Write", ToolOutput: "wrote 3 lines", Done: true},
	}

	restored, pending := replayRestoreForTest(rows, nil)
	require.Len(t, restored, 2)
	assert.Equal(t, restored[0].ToolName, "")
	assert.Equal(t, "Write", restored[1].ToolName)
	assert.NotEmpty(t, restored[1].ToolCallID, "synthetic fallback id must still be set")
	assert.Empty(t, pending["Write"])
}

// replayRestoreForTest reproduces the resume-path pairing loop in a pure
// helper so the behavior can be exercised without spinning up an engine.
// It mirrors the logic in the /resume handler: the event-log map wins when
// present, the FIFO queue threads ids from use to result, and a deterministic
// synthetic id stands in when the log is empty.
func replayRestoreForTest(msgs []store.MessageRow, msgIDToCallID map[int64]string) ([]engine.RestoredMessage, map[string][]string) {
	if msgIDToCallID == nil {
		msgIDToCallID = map[int64]string{}
	}
	restored := make([]engine.RestoredMessage, 0, len(msgs))
	pending := make(map[string][]string)
	for _, m := range msgs {
		rm := engine.RestoredMessage{
			Role:    m.Role,
			Content: m.Content,
		}
		switch m.Role {
		case "assistant":
			if m.ToolName != "" {
				callID := msgIDToCallID[m.ID]
				if callID == "" {
					callID = fmt.Sprintf("call_%s_%d", m.ToolName, len(restored))
				}
				pending[m.ToolName] = append(pending[m.ToolName], callID)
			}
		case "tool":
			callID := msgIDToCallID[m.ID]
			if callID == "" {
				if q := pending[m.ToolName]; len(q) > 0 {
					callID = q[0]
					pending[m.ToolName] = q[1:]
				}
			} else if q := pending[m.ToolName]; len(q) > 0 && q[0] == callID {
				pending[m.ToolName] = q[1:]
			}
			if callID == "" {
				callID = fmt.Sprintf("call_%s_%d", m.ToolName, len(restored))
			}
			rm.ToolName = m.ToolName
			rm.ToolInput = m.ToolArgs
			rm.ToolCallID = callID
			rm.Content = m.ToolOutput
		}
		restored = append(restored, rm)
	}
	return restored, pending
}

func mustToolCallIDEvent(t *testing.T, seq, messageID int64, tool, callID string) store.MessageEvent {
	t.Helper()
	payload := map[string]any{
		"message_id": messageID,
		"tool_name":  tool,
		"call_id":    callID,
		"role":       "tool",
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return store.MessageEvent{
		Seq:     seq,
		Kind:    store.EventKindToolCallID,
		Payload: string(raw),
	}
}
