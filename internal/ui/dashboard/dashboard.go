package dashboard

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Panel is a single dashboard panel with a title, glyph, and render function.
type Panel struct {
	ID        string
	Title     string
	Glyph     string // unicode icon prefix
	Collapsed bool
	Priority  int // lower = higher on screen
	Render    func(width int) string
	Badge     string // e.g. "[2 pending]" for approvals
	BadgeHot  bool   // true = flame red border on badge
}

// DashboardModel manages the vertical collapsible panel stack.
type DashboardModel struct {
	Panels   []Panel
	Width    int
	Height   int
	FocusIdx int  // which panel has keyboard focus for j/k nav
	Focused  bool // whether the dashboard pane is focused
}

// New creates a DashboardModel with default stub panels.
func New() DashboardModel {
	return DashboardModel{
		Panels: defaultPanels(),
	}
}

// SetSize updates the dashboard dimensions.
func (d *DashboardModel) SetSize(width, height int) {
	d.Width = width
	d.Height = height
}

// View renders the vertical collapsible panel stack inside a bordered box.
func (d DashboardModel) View() string {
	if d.Width < 8 || d.Height < 4 {
		return ""
	}

	var sections []string
	// Reserve 2 lines for top/bottom border.
	remainingH := d.Height - 2
	if remainingH < 1 {
		return ""
	}

	for i, panel := range d.Panels {
		if remainingH <= 0 {
			break
		}

		header := d.renderPanelHeader(panel, i == d.FocusIdx)
		if panel.Collapsed {
			sections = append(sections, header)
			remainingH--
			continue
		}

		// Inner width: total minus 2 for border chars.
		innerW := d.Width - 2
		if innerW < 4 {
			innerW = 4
		}

		body := "  No data"
		if panel.Render != nil {
			body = panel.Render(innerW)
		}

		bodyLines := strings.Split(body, "\n")
		available := remainingH - 1 // -1 for the header line
		if available <= 0 {
			sections = append(sections, header)
			break
		}
		if len(bodyLines) > available {
			bodyLines = bodyLines[:available]
		}

		sections = append(sections, header+"\n"+strings.Join(bodyLines, "\n"))
		remainingH -= 1 + len(bodyLines)
	}

	content := strings.Join(sections, "\n")

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3a2518")).
		Width(d.Width - 2). // -2 because lipgloss adds border chars
		Height(d.Height - 2)

	return border.Render(content)
}

// renderPanelHeader builds a single panel header line: glyph + title + badge.
func (d DashboardModel) renderPanelHeader(p Panel, focused bool) string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#D77757"))

	glyphStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFA600"))

	mutedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6b5040"))

	if focused && d.Focused {
		titleStyle = titleStyle.Foreground(lipgloss.Color("#FFA600"))
	}

	// Collapse indicator.
	arrow := "v"
	if p.Collapsed {
		arrow = ">"
	}

	header := mutedStyle.Render(arrow) + " " +
		glyphStyle.Render(p.Glyph) + " " +
		titleStyle.Render(p.Title)

	if p.Badge != "" {
		badgeStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6b5040"))
		if p.BadgeHot {
			badgeStyle = badgeStyle.
				Foreground(lipgloss.Color("#ff5555")).
				Bold(true)
		}
		header += " " + badgeStyle.Render(p.Badge)
	}

	return header
}

// Update handles dashboard-specific key events when the dashboard is focused.
func (d DashboardModel) Update(msg tea.Msg) (DashboardModel, tea.Cmd) {
	if !d.Focused {
		return d, nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			if d.FocusIdx < len(d.Panels)-1 {
				d.FocusIdx++
			}
		case "k", "up":
			if d.FocusIdx > 0 {
				d.FocusIdx--
			}
		case "enter", "space":
			if d.FocusIdx >= 0 && d.FocusIdx < len(d.Panels) {
				d.Panels[d.FocusIdx].Collapsed = !d.Panels[d.FocusIdx].Collapsed
			}
		}
	}
	return d, nil
}

// TogglePanel toggles the collapsed state of a panel by ID.
func (d *DashboardModel) TogglePanel(id string) {
	for i := range d.Panels {
		if d.Panels[i].ID == id {
			d.Panels[i].Collapsed = !d.Panels[i].Collapsed
			return
		}
	}
}

// SetPanelVisible sets a panel's collapsed state by ID.
func (d *DashboardModel) SetPanelVisible(id string, visible bool) {
	for i := range d.Panels {
		if d.Panels[i].ID == id {
			d.Panels[i].Collapsed = !visible
			return
		}
	}
}

// PanelByID returns a pointer to the panel with the given ID, or nil.
func (d *DashboardModel) PanelByID(id string) *Panel {
	for i := range d.Panels {
		if d.Panels[i].ID == id {
			return &d.Panels[i]
		}
	}
	return nil
}

// --- Default Panels (stubs - real renderers come in Phase 6 W2) ---

func defaultPanels() []Panel {
	return []Panel{
		{ID: "approvals", Title: "APPROVALS", Glyph: "!", Priority: 0,
			Render: func(w int) string { return "  No pending approvals" }},
		{ID: "agents", Title: "AGENTS", Glyph: "*", Priority: 1,
			Render: func(w int) string { return "  No active agents" }},
		{ID: "tokens", Title: "TOKENS", Glyph: "#", Priority: 2,
			Render: func(w int) string { return "  0% context used" }},
		{ID: "tasks", Title: "TASKS", Glyph: "+", Priority: 3,
			Render: func(w int) string { return "  No tasks" }},
		{ID: "files", Title: "FILES", Glyph: "=", Priority: 4,
			Render: func(w int) string { return "  No files touched" }},
		{ID: "errors", Title: "ERRORS", Glyph: "x", Priority: 5, Collapsed: true,
			Render: func(w int) string { return "  No errors" }},
		{ID: "compact", Title: "COMPACT", Glyph: "~", Priority: 6, Collapsed: true,
			Render: func(w int) string { return "  Idle" }},
		{ID: "hooks", Title: "HOOKS", Glyph: "o", Priority: 7, Collapsed: true,
			Render: func(w int) string { return "  No hooks firing" }},
	}
}
