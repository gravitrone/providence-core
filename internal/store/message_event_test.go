package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddAndGetMessageEvents(t *testing.T) {
	s := testStore(t)
	require.NoError(t, s.CreateSession("s1", "/tmp", "direct", "sonnet"))

	_, err := s.AddMessageEvent("s1", 1, EventKindToolCallID, `{"tool_name":"Read","call_id":"call_a"}`)
	require.NoError(t, err)
	_, err = s.AddMessageEvent("s1", 2, EventKindFileSnapshot, `{"tool":"Write"}`)
	require.NoError(t, err)
	_, err = s.AddMessageEvent("s1", 3, EventKindWorktree, `{"total":7}`)
	require.NoError(t, err)

	events, err := s.GetSessionEvents("s1")
	require.NoError(t, err)
	require.Len(t, events, 3)
	assert.Equal(t, EventKindToolCallID, events[0].Kind)
	assert.Equal(t, int64(1), events[0].Seq)
	assert.Equal(t, EventKindWorktree, events[2].Kind)
}

func TestGetSessionEventsEmptyIsNil(t *testing.T) {
	s := testStore(t)
	require.NoError(t, s.CreateSession("s1", "/tmp", "direct", "sonnet"))
	events, err := s.GetSessionEvents("s1")
	require.NoError(t, err)
	assert.Empty(t, events)
}

func TestDeleteSessionEventsCascade(t *testing.T) {
	s := testStore(t)
	require.NoError(t, s.CreateSession("s1", "/tmp", "direct", "sonnet"))
	_, err := s.AddMessageEvent("s1", 1, EventKindToolCallID, `{}`)
	require.NoError(t, err)

	require.NoError(t, s.DeleteSessionEvents("s1"))
	events, err := s.GetSessionEvents("s1")
	require.NoError(t, err)
	assert.Empty(t, events)
}
