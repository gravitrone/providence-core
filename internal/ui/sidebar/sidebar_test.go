package sidebar

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSidebar(t *testing.T) {
	m := New()
	assert.Equal(t, 0.0, m.Position)
	assert.False(t, m.HasAgents())
	assert.Empty(t, m.VisibleAgents())
}

func TestHasAgentsRunning(t *testing.T) {
	m := New()
	m.Update([]AgentCard{
		{ID: "a1", Name: "test", Status: "running"},
	})
	assert.True(t, m.HasAgents())
}

func TestHasAgentsCompletedGrace(t *testing.T) {
	m := New()
	m.Update([]AgentCard{
		{ID: "a1", Name: "test", Status: "completed"},
	})
	// Just completed - within grace period.
	assert.True(t, m.HasAgents())
}

func TestHasAgentsEmpty(t *testing.T) {
	m := New()
	m.Update(nil)
	assert.False(t, m.HasAgents())
}

func TestEffectiveWidth(t *testing.T) {
	m := New()
	m.Position = 0.5
	assert.Equal(t, 15, m.EffectiveWidth(30))
	m.Position = 1.0
	assert.Equal(t, 30, m.EffectiveWidth(30))
	m.Position = 0.0
	assert.Equal(t, 0, m.EffectiveWidth(30))
}

func TestViewMinSize(t *testing.T) {
	m := New()
	// Too small should return empty.
	assert.Equal(t, "", m.View(3, 3, 0))
	assert.Equal(t, "", m.View(10, 2, 0))
}

func TestViewRendersAgents(t *testing.T) {
	m := New()
	m.Update([]AgentCard{
		{ID: "a1", Name: "explorer", Status: "running", Activity: "Reading files..."},
	})
	view := m.View(25, 10, 0)
	require.NotEmpty(t, view)
	assert.Contains(t, view, "Agents")
	assert.Contains(t, view, "explorer")
}

func TestViewRendersCompleted(t *testing.T) {
	m := New()
	m.Update([]AgentCard{
		{ID: "a1", Name: "finder", Status: "completed", Elapsed: 8 * time.Second},
	})
	view := m.View(25, 10, 0)
	require.NotEmpty(t, view)
	assert.Contains(t, view, "finder")
	assert.Contains(t, view, "DONE")
}

func TestTickSpringsToTarget(t *testing.T) {
	m := New()
	m.Update([]AgentCard{
		{ID: "a1", Name: "test", Status: "running"},
	})

	// Tick multiple times - position should increase toward 1.0.
	for i := 0; i < 30; i++ {
		m.Tick()
	}
	assert.Greater(t, m.Position, 0.5, "position should be moving toward 1.0")
}

func TestTickSpringsBack(t *testing.T) {
	m := New()
	m.Position = 1.0

	// No agents - should spring back to 0.0.
	m.Update(nil)
	for i := 0; i < 30; i++ {
		m.Tick()
	}
	assert.Less(t, m.Position, 0.5, "position should be moving toward 0.0")
}

func TestFormatElapsed(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Millisecond, "<1s"},
		{5 * time.Second, "5s"},
		{90 * time.Second, "1m30s"},
		{3600 * time.Second, "1h0m"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, FormatElapsed(tt.d))
	}
}

func TestStatusIcon(t *testing.T) {
	// Just verify icons render non-empty strings.
	assert.NotEmpty(t, StatusIcon("running", 0))
	assert.NotEmpty(t, StatusIcon("completed", 0))
	assert.NotEmpty(t, StatusIcon("failed", 0))
	assert.NotEmpty(t, StatusIcon("killed", 0))
	assert.NotEmpty(t, StatusIcon("unknown", 0))
}
