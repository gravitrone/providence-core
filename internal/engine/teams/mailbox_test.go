package teams

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tempMailbox(t *testing.T) (*Mailbox, *Store) {
	t.Helper()
	s := tempStore(t)
	_, err := s.CreateTeam("test-team", "test")
	require.NoError(t, err)
	return NewMailbox(s), s
}

func TestMailboxWriteAndReadUnread(t *testing.T) {
	mb, _ := tempMailbox(t)

	msg := Message{
		From:      "alice",
		Text:      "hey bob, check the tests",
		Timestamp: time.Now(),
		Read:      false,
		Summary:   "test request",
	}
	err := mb.WriteToMailbox("test-team", "bob", msg)
	require.NoError(t, err)

	unread, err := mb.ReadUnread("test-team", "bob")
	require.NoError(t, err)
	require.Len(t, unread, 1)
	assert.Equal(t, "alice", unread[0].From)
	assert.Equal(t, "hey bob, check the tests", unread[0].Text)
	assert.Equal(t, "test request", unread[0].Summary)
}

func TestMailboxMultipleMessages(t *testing.T) {
	mb, _ := tempMailbox(t)

	for i, text := range []string{"first", "second", "third"} {
		err := mb.WriteToMailbox("test-team", "bob", Message{
			From:      "alice",
			Text:      text,
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
		})
		require.NoError(t, err)
	}

	unread, err := mb.ReadUnread("test-team", "bob")
	require.NoError(t, err)
	assert.Len(t, unread, 3)
	assert.Equal(t, "first", unread[0].Text)
	assert.Equal(t, "third", unread[2].Text)
}

func TestMailboxMarkRead(t *testing.T) {
	mb, _ := tempMailbox(t)

	err := mb.WriteToMailbox("test-team", "charlie", Message{
		From: "alice",
		Text: "ping",
	})
	require.NoError(t, err)

	err = mb.MarkRead("test-team", "charlie")
	require.NoError(t, err)

	unread, err := mb.ReadUnread("test-team", "charlie")
	require.NoError(t, err)
	assert.Empty(t, unread)
}

func TestMailboxMarkReadIdempotent(t *testing.T) {
	mb, _ := tempMailbox(t)

	// Mark read on empty inbox should be fine.
	err := mb.MarkRead("test-team", "nobody")
	assert.NoError(t, err)
}

func TestMailboxClearInbox(t *testing.T) {
	mb, _ := tempMailbox(t)

	err := mb.WriteToMailbox("test-team", "dave", Message{
		From: "alice",
		Text: "hi",
	})
	require.NoError(t, err)

	err = mb.ClearInbox("test-team", "dave")
	require.NoError(t, err)

	unread, err := mb.ReadUnread("test-team", "dave")
	require.NoError(t, err)
	assert.Empty(t, unread)
}

func TestMailboxClearInboxNotExists(t *testing.T) {
	mb, _ := tempMailbox(t)

	// Clearing nonexistent inbox is a no-op.
	err := mb.ClearInbox("test-team", "ghost")
	assert.NoError(t, err)
}

func TestMailboxReadUnreadEmptyInbox(t *testing.T) {
	mb, _ := tempMailbox(t)

	unread, err := mb.ReadUnread("test-team", "new-agent")
	require.NoError(t, err)
	assert.Empty(t, unread)
}

func TestMailboxReadPreservesExistingRead(t *testing.T) {
	mb, _ := tempMailbox(t)

	// Write two messages.
	err := mb.WriteToMailbox("test-team", "eve", Message{
		From: "alice",
		Text: "first",
	})
	require.NoError(t, err)

	// Mark first as read.
	err = mb.MarkRead("test-team", "eve")
	require.NoError(t, err)

	// Write another.
	err = mb.WriteToMailbox("test-team", "eve", Message{
		From: "bob",
		Text: "second",
	})
	require.NoError(t, err)

	// Only the new one should be unread.
	unread, err := mb.ReadUnread("test-team", "eve")
	require.NoError(t, err)
	assert.Len(t, unread, 1)
	assert.Equal(t, "second", unread[0].Text)
}
