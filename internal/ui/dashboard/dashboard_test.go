package dashboard

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keyPress builds a tea.KeyPressMsg for testing.
func keyPress(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case " ":
		return tea.KeyPressMsg{Code: tea.KeySpace}
	default:
		return tea.KeyPressMsg{Text: key}
	}
}

func newTestDashboard(w, h int) DashboardModel {
	d := New()
	d.SetSize(w, h)
	return d
}

func TestDashboardView(t *testing.T) {
	d := newTestDashboard(40, 30)
	view := d.View()
	require.NotEmpty(t, view, "dashboard view should produce output")

	// All 8 default panels should have their titles present.
	for _, p := range d.Panels {
		assert.Contains(t, view, p.Title, "view should contain panel title %q", p.Title)
	}
}

func TestDashboardCollapse(t *testing.T) {
	d := newTestDashboard(40, 30)

	// APPROVALS is not collapsed by default - its header should appear.
	view := d.View()
	assert.Contains(t, view, "APPROVALS", "expanded panel header should appear")

	// Collapse it.
	d.Panels[0].Collapsed = true
	view = d.View()
	// Title header still shows.
	assert.Contains(t, view, "APPROVALS", "collapsed panel header should still show")
	// The "▸" collapse indicator should be used for this panel.
	lines := strings.Split(view, "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, "APPROVALS") && strings.Contains(line, "▸") {
			found = true
			break
		}
	}
	assert.True(t, found, "collapsed panel should show ▸ indicator")
}

func TestDashboardNavigation(t *testing.T) {
	d := newTestDashboard(40, 30)
	d.Focused = true

	assert.Equal(t, 0, d.FocusIdx, "initial focus should be 0")

	// j moves down.
	d, _ = d.Update(keyPress("j"))
	assert.Equal(t, 1, d.FocusIdx, "j should move focus down")

	// j again.
	d, _ = d.Update(keyPress("j"))
	assert.Equal(t, 2, d.FocusIdx, "j should move focus down again")

	// k moves up.
	d, _ = d.Update(keyPress("k"))
	assert.Equal(t, 1, d.FocusIdx, "k should move focus up")

	// k at 0 stays at 0.
	d.FocusIdx = 0
	d, _ = d.Update(keyPress("k"))
	assert.Equal(t, 0, d.FocusIdx, "k at 0 should stay at 0")

	// j at last panel stays at last.
	d.FocusIdx = len(d.Panels) - 1
	d, _ = d.Update(keyPress("j"))
	assert.Equal(t, len(d.Panels)-1, d.FocusIdx, "j at last should stay at last")
}

func TestDashboardToggle(t *testing.T) {
	d := newTestDashboard(40, 30)
	d.Focused = true
	d.FocusIdx = 0

	// First panel starts expanded.
	assert.False(t, d.Panels[0].Collapsed, "panel should start expanded")

	// Enter toggles collapse.
	d, _ = d.Update(keyPress("enter"))
	assert.True(t, d.Panels[0].Collapsed, "enter should collapse focused panel")

	// Enter again toggles back.
	d, _ = d.Update(keyPress("enter"))
	assert.False(t, d.Panels[0].Collapsed, "enter again should expand panel")

	// Space also works.
	d, _ = d.Update(keyPress(" "))
	assert.True(t, d.Panels[0].Collapsed, "space should toggle collapse")
}

func TestDashboardWidthRespected(t *testing.T) {
	tests := []struct {
		name  string
		width int
	}{
		{"narrow", 20},
		{"medium", 40},
		{"wide", 80},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newTestDashboard(tt.width, 30)
			view := d.View()
			assert.NotEmpty(t, view, "view should produce output at width %d", tt.width)
		})
	}
}

func TestDashboardHeightTruncation(t *testing.T) {
	// Very short height - should not render all panels.
	d := newTestDashboard(40, 8)
	view := d.View()
	require.NotEmpty(t, view)

	// Count visible lines.
	lines := strings.Split(view, "\n")
	// Should respect height constraint: total lines <= height.
	assert.LessOrEqual(t, len(lines), d.Height+1,
		"rendered lines should not massively exceed height (got %d, height %d)", len(lines), d.Height)
}

func TestDashboardNotFocusedIgnoresKeys(t *testing.T) {
	d := newTestDashboard(40, 30)
	d.Focused = false
	d.FocusIdx = 0

	// Keys should be ignored when not focused.
	d, _ = d.Update(keyPress("j"))
	assert.Equal(t, 0, d.FocusIdx, "unfocused dashboard should ignore j key")
}

func TestDashboardPanelByID(t *testing.T) {
	d := newTestDashboard(40, 30)

	p := d.PanelByID("tokens")
	require.NotNil(t, p)
	assert.Equal(t, "TOKENS", p.Title)

	p = d.PanelByID("nonexistent")
	assert.Nil(t, p)
}

func TestDashboardTogglePanelByID(t *testing.T) {
	d := newTestDashboard(40, 30)

	// Agents panel starts expanded.
	p := d.PanelByID("agents")
	require.NotNil(t, p)
	assert.False(t, p.Collapsed)

	d.TogglePanel("agents")
	p = d.PanelByID("agents")
	assert.True(t, p.Collapsed)
}

func TestDashboardEmptyOnTinySize(t *testing.T) {
	d := newTestDashboard(4, 2)
	view := d.View()
	assert.Empty(t, view, "tiny dashboard should render empty")
}

// --- Agent Panel Tree Tests ---

func TestDashboardAgentPanelRendersTree(t *testing.T) {
	d := newTestDashboard(60, 30)
	d.SetAgents([]AgentInfo{
		{Name: "fix-auth-bug", Model: "opus", Status: "running", Elapsed: "12s", LastActivity: "Reading internal/auth/..."},
		{Name: "verify-tests", Model: "sonnet", Status: "completed", Elapsed: "8s", LastActivity: "PASS (14 checks)"},
	})

	view := d.View()
	assert.Contains(t, view, "fix-auth-bug", "should render agent name")
	assert.Contains(t, view, "verify-tests", "should render second agent name")
	assert.Contains(t, view, "[opus]", "should show model")
	assert.Contains(t, view, "[sonnet]", "should show model")
	assert.Contains(t, view, "12s", "should show elapsed time")
}

func TestDashboardAgentPanelStatusIcons(t *testing.T) {
	d := newTestDashboard(60, 30)
	d.SetAgents([]AgentInfo{
		{Name: "runner", Status: "running", Elapsed: "5s", Model: "opus"},
		{Name: "done", Status: "completed", Elapsed: "3s", Model: "sonnet"},
		{Name: "broken", Status: "failed", Elapsed: "1s", Model: "haiku"},
	})

	view := d.View()
	// Running: ● (U+25CF), Completed: ✓ (U+2713), Failed: × (U+00D7).
	assert.Contains(t, view, "\u25CF", "running agent should show ● icon")
	assert.Contains(t, view, "\u2713", "completed agent should show ✓ icon")
	assert.Contains(t, view, "\u00D7", "failed agent should show × icon")
}

func TestDashboardAgentPanelHierarchical(t *testing.T) {
	d := newTestDashboard(60, 30)
	d.SetAgents([]AgentInfo{
		{Name: "parent", Model: "opus", Status: "running", Elapsed: "10s"},
		{Name: "child", Model: "haiku", Status: "running", Elapsed: "3s", ParentName: "parent"},
	})

	view := d.View()
	assert.Contains(t, view, "parent", "should contain parent agent")
	assert.Contains(t, view, "child", "should contain child agent")
}

func TestDashboardAgentPanelActivityLine(t *testing.T) {
	d := newTestDashboard(60, 30)
	d.SetAgents([]AgentInfo{
		{Name: "worker", Model: "opus", Status: "running", Elapsed: "5s", LastActivity: "Editing auth.go"},
	})

	view := d.View()
	// Activity line uses ⎿ connector (U+23BF).
	assert.Contains(t, view, "\u23BF", "should show activity connector")
	assert.Contains(t, view, "Editing auth.go", "should show activity text")
}

func TestDashboardAgentPanelBadge(t *testing.T) {
	d := newTestDashboard(60, 30)
	d.SetAgents([]AgentInfo{
		{Name: "a1", Status: "running", Elapsed: "1s", Model: "opus"},
		{Name: "a2", Status: "running", Elapsed: "2s", Model: "opus"},
		{Name: "a3", Status: "completed", Elapsed: "3s", Model: "sonnet"},
	})

	p := d.PanelByID("agents")
	require.NotNil(t, p)
	assert.Equal(t, "[2 active]", p.Badge, "badge should show active agent count")
}

func TestDashboardAgentPanelEmpty(t *testing.T) {
	d := newTestDashboard(60, 30)
	d.SetAgents([]AgentInfo{})

	p := d.PanelByID("agents")
	require.NotNil(t, p)
	assert.Empty(t, p.Badge, "empty agent list should have no badge")
}

func TestDashboardAgentsTabRendersTree(t *testing.T) {
	d := newTestDashboard(80, 40)
	d.SetAgents([]AgentInfo{
		{Name: "fix-auth-bug", Model: "opus", Status: "running", Elapsed: "12s"},
		{Name: "verify-tests", Model: "sonnet", Status: "completed", Elapsed: "8s"},
	})

	view := d.RenderAgentsTab(80, 40)
	assert.Contains(t, view, "fix-auth-bug")
	assert.Contains(t, view, "verify-tests")
}

func TestDashboardAgentsTabEmpty(t *testing.T) {
	d := newTestDashboard(80, 40)
	view := d.RenderAgentsTab(80, 40)
	assert.Contains(t, view, "No active agents")
}
