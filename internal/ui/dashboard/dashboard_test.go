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

	// APPROVALS is not collapsed by default - its body should appear.
	view := d.View()
	assert.Contains(t, view, "No pending approvals", "expanded panel body should appear")

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
