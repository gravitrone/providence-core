package store

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenAndMigrate(t *testing.T) {
	s := testStore(t)
	assert.NotNil(t, s)
}

func TestCreateAndGetSession(t *testing.T) {
	s := testStore(t)
	err := s.CreateSession("s1", "/tmp/project", "direct", "sonnet")
	require.NoError(t, err)

	session, err := s.GetSession("s1")
	require.NoError(t, err)
	assert.Equal(t, "s1", session.ID)
	assert.Equal(t, "/tmp/project", session.CWD)
	assert.Equal(t, "direct", session.EngineType)
	assert.Equal(t, "sonnet", session.Model)
}

func TestUpdateSessionTitle(t *testing.T) {
	s := testStore(t)
	s.CreateSession("s1", "/tmp", "claude", "haiku")
	s.UpdateSessionTitle("s1", "my cool session")

	session, _ := s.GetSession("s1")
	assert.Equal(t, "my cool session", session.Title)
}

func TestDeleteSession(t *testing.T) {
	s := testStore(t)
	s.CreateSession("s1", "/tmp", "claude", "haiku")
	s.AddMessage("s1", "user", "hello", "", "", "", "", "", 0, true)
	s.AddMessage("s1", "assistant", "hi", "", "", "", "", "", 0, true)

	err := s.DeleteSession("s1")
	require.NoError(t, err)

	// Session gone
	session, err := s.GetSession("s1")
	assert.Error(t, err)
	assert.Nil(t, session)

	// Messages cascaded
	msgs, _ := s.GetMessages("s1")
	assert.Empty(t, msgs)
}

func TestListSessions(t *testing.T) {
	s := testStore(t)
	s.CreateSession("s1", "/project-a", "claude", "sonnet")
	s.CreateSession("s2", "/project-a", "direct", "opus")
	s.CreateSession("s3", "/project-b", "claude", "haiku")

	// List all
	all, err := s.ListSessions("", 10)
	require.NoError(t, err)
	assert.Len(t, all, 3)

	// List by CWD
	projA, err := s.ListSessions("/project-a", 10)
	require.NoError(t, err)
	assert.Len(t, projA, 2)

	projB, err := s.ListSessions("/project-b", 10)
	require.NoError(t, err)
	assert.Len(t, projB, 1)
}

func TestListSessionsWithMessageCount(t *testing.T) {
	s := testStore(t)
	s.CreateSession("s1", "/tmp", "claude", "sonnet")
	s.AddMessage("s1", "user", "hello", "", "", "", "", "", 0, true)
	s.AddMessage("s1", "assistant", "hi", "", "", "", "", "", 0, true)
	s.AddMessage("s1", "user", "bye", "", "", "", "", "", 0, true)

	sessions, _ := s.ListSessions("", 10)
	require.Len(t, sessions, 1)
	assert.Equal(t, 3, sessions[0].MessageCount)
}

func TestAddAndGetMessages(t *testing.T) {
	s := testStore(t)
	s.CreateSession("s1", "/tmp", "claude", "sonnet")

	id1, err := s.AddMessage("s1", "user", "find bugs", "", "", "", "", "", 0, true)
	require.NoError(t, err)
	assert.Greater(t, id1, int64(0))

	id2, _ := s.AddMessage("s1", "tool", "", "Bash", "ls -la", "success", "Holy fire...", "file1.go\nfile2.go", 0, true)
	assert.Greater(t, id2, id1)

	msgs, err := s.GetMessages("s1")
	require.NoError(t, err)
	require.Len(t, msgs, 2)

	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "find bugs", msgs[0].Content)

	assert.Equal(t, "tool", msgs[1].Role)
	assert.Equal(t, "Bash", msgs[1].ToolName)
	assert.Equal(t, "ls -la", msgs[1].ToolArgs)
	assert.Equal(t, "success", msgs[1].ToolStatus)
	assert.Equal(t, "file1.go\nfile2.go", msgs[1].ToolOutput)
}

func TestUpdateMessageContent(t *testing.T) {
	s := testStore(t)
	s.CreateSession("s1", "/tmp", "direct", "sonnet")

	id, _ := s.AddMessage("s1", "assistant", "partial...", "", "", "", "", "", 0, false)

	err := s.UpdateMessageContent(id, "full response here", true)
	require.NoError(t, err)

	msgs, _ := s.GetMessages("s1")
	require.Len(t, msgs, 1)
	assert.Equal(t, "full response here", msgs[0].Content)
	assert.True(t, msgs[0].Done)
}

func TestUpdateToolOutput(t *testing.T) {
	s := testStore(t)
	s.CreateSession("s1", "/tmp", "direct", "sonnet")

	id, _ := s.AddMessage("s1", "tool", "", "Read", "main.go", "pending", "", "", 0, true)

	err := s.UpdateToolOutput(id, "package main\n\nfunc main() {}", "success")
	require.NoError(t, err)

	msgs, _ := s.GetMessages("s1")
	require.Len(t, msgs, 1)
	assert.Equal(t, "package main\n\nfunc main() {}", msgs[0].ToolOutput)
	assert.Equal(t, "success", msgs[0].ToolStatus)
}

func TestNilStoreSafe(t *testing.T) {
	var s *Store
	// All methods should be nil-safe
	assert.NoError(t, s.CreateSession("x", "/", "c", "m"))
	assert.NoError(t, s.UpdateSessionTitle("x", "t"))
	assert.NoError(t, s.DeleteSession("x"))
	assert.NoError(t, s.Close())

	id, err := s.AddMessage("x", "user", "hi", "", "", "", "", "", 0, true)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), id)

	sessions, err := s.ListSessions("", 10)
	assert.NoError(t, err)
	assert.Nil(t, sessions)

	msgs, err := s.GetMessages("x")
	assert.NoError(t, err)
	assert.Nil(t, msgs)
}

func TestFTS5Search(t *testing.T) {
	s := testStore(t)
	require.NoError(t, s.CreateSession("s1", "/tmp/project-a", "claude", "sonnet"))
	require.NoError(t, s.CreateSession("s2", "/tmp/project-b", "direct", "opus"))

	_, err := s.AddMessage("s1", "user", "providence flame dashboard", "", "", "", "", "", 0, true)
	require.NoError(t, err)
	_, err = s.AddMessage("s1", "assistant", "completely unrelated reply", "", "", "", "", "", 0, true)
	require.NoError(t, err)
	_, err = s.AddMessage("s2", "assistant", "providence session bus uses sqlite search", "", "", "", "", "", 0, true)
	require.NoError(t, err)

	results, err := s.SearchMessages("providence", 10)
	require.NoError(t, err)
	require.Len(t, results, 2)

	sessionIDs := []string{results[0].SessionID, results[1].SessionID}
	assert.ElementsMatch(t, []string{"s1", "s2"}, sessionIDs)
	for _, result := range results {
		assert.Contains(t, result.Content, "providence")
		assert.Contains(t, result.Snippet, "<mark>providence</mark>")
	}
}

func TestFTS5NoResults(t *testing.T) {
	s := testStore(t)
	require.NoError(t, s.CreateSession("s1", "/tmp/project", "claude", "sonnet"))
	_, err := s.AddMessage("s1", "user", "embers and flames only", "", "", "", "", "", 0, true)
	require.NoError(t, err)

	results, err := s.SearchMessages("nonexistent", 10)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestCascadeDelete(t *testing.T) {
	s := testStore(t)
	require.NoError(t, s.CreateSession("s1", "/tmp/project", "claude", "sonnet"))
	_, err := s.AddMessage("s1", "user", "searchable providence content", "", "", "", "", "", 0, true)
	require.NoError(t, err)
	_, err = s.AddMessage("s1", "assistant", "another searchable providence reply", "", "", "", "", "", 0, true)
	require.NoError(t, err)

	results, err := s.SearchMessages("providence", 10)
	require.NoError(t, err)
	require.Len(t, results, 2)

	require.NoError(t, s.DeleteSession("s1"))

	var messageCount int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id = ?`, "s1").Scan(&messageCount)
	require.NoError(t, err)
	assert.Zero(t, messageCount)

	msgs, err := s.GetMessages("s1")
	require.NoError(t, err)
	assert.Empty(t, msgs)

	results, err = s.SearchMessages("providence", 10)
	require.NoError(t, err)
	assert.Empty(t, results)
}
