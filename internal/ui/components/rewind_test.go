package components

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testItems() []RewindItem {
	return []RewindItem{
		{UUID: "a", Role: "user", Preview: "fix the auth bug", Index: 0},
		{UUID: "b", Role: "user", Preview: "now add tests for it", Index: 2},
		{UUID: "c", Role: "user", Preview: "deploy to staging", Index: 4},
		{UUID: "d", Role: "user", Preview: "check the logs", Index: 6},
		{UUID: "e", Role: "user", Preview: "rollback the deployment", Index: 8},
	}
}

func TestRewindNavigation(t *testing.T) {
	tests := []struct {
		name     string
		keys     []string
		wantSel  int
	}{
		{"j moves down", []string{"j"}, 1},
		{"k at top stays", []string{"k"}, 0},
		{"down arrow moves down", []string{"down"}, 1},
		{"j j moves to 2", []string{"j", "j"}, 2},
		{"j k returns to 1 then 0", []string{"j", "j", "k"}, 1},
		{"up moves up after down", []string{"j", "j", "up"}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewRewindModel(testItems(), 80)
			for _, k := range tt.keys {
				var handled bool
				m, _, handled = m.HandleKey(k)
				assert.True(t, handled, "key %q should be handled", k)
			}
			assert.Equal(t, tt.wantSel, m.Selected())
		})
	}
}

func TestRewindNavigationBounds(t *testing.T) {
	m := NewRewindModel(testItems(), 80)
	// Navigate past the end.
	for i := 0; i < 20; i++ {
		m, _, _ = m.HandleKey("j")
	}
	assert.Equal(t, len(testItems())-1, m.Selected(), "should not exceed last item")

	// Navigate past the start.
	for i := 0; i < 20; i++ {
		m, _, _ = m.HandleKey("k")
	}
	assert.Equal(t, 0, m.Selected(), "should not go below 0")
}

func TestRewindConfirmMode(t *testing.T) {
	m := NewRewindModel(testItems(), 80)
	assert.False(t, m.ConfirmMode())

	// Move to second item and enter confirm mode.
	m, _, _ = m.HandleKey("j")
	m, msg, handled := m.HandleKey("enter")
	assert.True(t, handled)
	assert.Nil(t, msg, "enter into confirm should not emit msg yet")
	assert.True(t, m.ConfirmMode())

	// Navigate confirm options.
	m, _, _ = m.HandleKey("j") // move to "Summarize from here"
	m, msg, _ = m.HandleKey("enter")
	require.NotNil(t, msg)
	assert.Equal(t, RewindSummarize, msg.Action)
	assert.Equal(t, 2, msg.Index, "should be original index of second item")
}

func TestRewindConfirmRestore(t *testing.T) {
	m := NewRewindModel(testItems(), 80)
	m, _, _ = m.HandleKey("enter") // confirm mode
	m, msg, _ := m.HandleKey("enter") // select first option (restore)
	require.NotNil(t, msg)
	assert.Equal(t, RewindRestore, msg.Action)
	assert.Equal(t, 0, msg.Index)
	assert.False(t, m.Active(), "picker should close after confirm")
}

func TestRewindConfirmCancel(t *testing.T) {
	m := NewRewindModel(testItems(), 80)
	m, _, _ = m.HandleKey("enter")
	m, _, _ = m.HandleKey("j") // summarize
	m, _, _ = m.HandleKey("j") // never mind
	m, msg, _ := m.HandleKey("enter")
	require.NotNil(t, msg)
	assert.Equal(t, RewindCancel, msg.Action)
}

func TestRewindConfirmEscReturnsToList(t *testing.T) {
	m := NewRewindModel(testItems(), 80)
	m, _, _ = m.HandleKey("enter") // confirm mode
	assert.True(t, m.ConfirmMode())
	m, _, _ = m.HandleKey("esc") // back to list
	assert.False(t, m.ConfirmMode())
	assert.True(t, m.Active(), "picker should still be open")
}

func TestRewindDismiss(t *testing.T) {
	m := NewRewindModel(testItems(), 80)
	assert.True(t, m.Active())

	m, msg, handled := m.HandleKey("esc")
	assert.True(t, handled)
	require.NotNil(t, msg)
	assert.Equal(t, RewindCancel, msg.Action)
	assert.False(t, m.Active())
}

func TestRewindViewNotEmpty(t *testing.T) {
	m := NewRewindModel(testItems(), 80)
	view := m.View()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "fix the auth bug")
	assert.Contains(t, view, "/rewind")
}

func TestRewindViewConfirmMode(t *testing.T) {
	m := NewRewindModel(testItems(), 80)
	m, _, _ = m.HandleKey("enter")
	view := m.View()
	assert.Contains(t, view, "Restore conversation")
	assert.Contains(t, view, "Summarize from here")
	assert.Contains(t, view, "Never mind")
}

func TestRewindEmptyMessages(t *testing.T) {
	m := NewRewindModel(nil, 80)
	view := m.View()
	assert.Empty(t, view)
}

func TestRewindUnhandledKey(t *testing.T) {
	m := NewRewindModel(testItems(), 80)
	_, _, handled := m.HandleKey("x")
	assert.False(t, handled)
}
