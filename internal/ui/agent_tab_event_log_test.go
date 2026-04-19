package ui

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gravitrone/providence-core/internal/config"
	"github.com/gravitrone/providence-core/internal/store"
)

// TestSessionResumeHydratesEventLog exercises the round trip between the
// event-log writer and the resume-time hydrator. A fresh store is populated
// with tool_call_id, file_snapshot, content_replacement, and worktree events;
// the hydrator is then expected to rebuild fileHistory, contentReplacements,
// worktreeState, and toolCallIDs on an AgentTab.
func TestSessionResumeHydratesEventLog(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "events.db")
	s, err := store.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, s.Close())
	})

	const sessionID = "sess-resume"
	require.NoError(t, s.CreateSession(sessionID, "/tmp/project", "direct", "sonnet"))

	// Two tool_use rows for the same tool so the resume path has something
	// worth pairing by id instead of by name+index.
	mustEvent(t, s, sessionID, 1, store.EventKindToolCallID, map[string]any{
		"tool_name": "Read",
		"call_id":   "call_Read_a",
		"role":      "tool",
	})
	mustEvent(t, s, sessionID, 2, store.EventKindToolCallID, map[string]any{
		"tool_name": "Read",
		"call_id":   "call_Read_b",
		"role":      "tool",
	})
	mustEvent(t, s, sessionID, 3, store.EventKindFileSnapshot, map[string]any{
		"tool":    "Write",
		"call_id": "call_Write_1",
		"output":  "wrote 12 lines",
	})
	mustEvent(t, s, sessionID, 4, store.EventKindContentReplacement, map[string]any{
		"tool":    "Read",
		"call_id": "call_Read_a",
		"bytes":   8192,
	})
	mustEvent(t, s, sessionID, 5, store.EventKindWorktree, map[string]any{
		"path":  "/tmp/project/.providence/worktree-index.json",
		"total": 142,
	})

	events, err := s.GetSessionEvents(sessionID)
	require.NoError(t, err)
	require.Len(t, events, 5)

	at := NewAgentTab("", config.Config{}, s, nil)
	at.sessionID = sessionID
	at.hydrateFromEvents(events)

	// Most recent tool_call_id for Read wins, matching the in-memory model
	// where the map tracks the latest invocation per tool name.
	assert.Equal(t, "call_Read_b", at.toolCallIDs["Read"])
	assert.Len(t, at.fileHistory, 1, "file_snapshot events must populate fileHistory")
	assert.Equal(t, int64(3), at.fileHistory[0].Seq)
	assert.Contains(t, at.contentReplacements, "call_Read_a")
	assert.NotEmpty(t, at.worktreeState, "worktree payload must be captured verbatim")
	assert.Equal(t, int64(5), at.eventSeq, "eventSeq must advance past the highest persisted row")
}

// TestSessionResumeHydrateEmptyIsNoError verifies the back-compat branch:
// sessions written before the event log existed hand back an empty slice and
// the hydrator clears state without failing.
func TestSessionResumeHydrateEmptyIsNoError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "empty.db")
	s, err := store.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, s.Close())
	})

	at := NewAgentTab("", config.Config{}, s, nil)
	// Pre-seed bogus state so we can prove hydrate clears it.
	at.toolCallIDs = map[string]string{"Old": "stale"}
	at.contentReplacements = map[string]string{"x": "y"}
	at.worktreeState = []byte("stale")
	at.eventSeq = 99

	events, err := s.GetSessionEvents("nonexistent-session")
	require.NoError(t, err)
	assert.Empty(t, events)

	at.hydrateFromEvents(events)
	assert.Empty(t, at.toolCallIDs)
	assert.Empty(t, at.contentReplacements)
	assert.Empty(t, at.worktreeState)
	assert.Equal(t, int64(0), at.eventSeq)
}

func mustEvent(t *testing.T, s *store.Store, sessionID string, seq int64, kind string, payload any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	_, err = s.AddMessageEvent(sessionID, seq, kind, string(raw))
	require.NoError(t, err)
}
