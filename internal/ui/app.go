package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gravitrone/providence-core/internal/config"
	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/store"
	"github.com/gravitrone/providence-core/internal/ui/components"
)

// App is the root TUI model. Providence is purely the agent chat - no tabs.
type App struct {
	keys     KeyMap
	width    int
	height   int
	agentTab AgentTab
}

// NewApp creates and returns a new App model.
// engineType sets the initial AI backend; pass "" for the default (claude).
func NewApp(engineType string, cfg config.Config, st *store.Store) App {
	// Restore persisted theme before constructing the agent tab so the
	// renderer picks up the correct palette on first paint.
	switch cfg.Theme {
	case "flame", "night":
		ApplyTheme(cfg.Theme)
	case "auto":
		hour := time.Now().Hour()
		name := "flame"
		if hour < 6 || hour >= 18 {
			name = "night"
		}
		ApplyTheme(name)
	}
	return App{
		keys:     DefaultKeyMap(),
		agentTab: NewAgentTab(engine.EngineType(engineType), cfg, st),
	}
}

// Init implements tea.Model.
func (a App) Init() tea.Cmd {
	return flameTick()
}

// Update implements tea.Model.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.agentTab.Resize(a.width, a.height)
		return a, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}
	}

	// Delegate all messages to agent tab.
	updated, cmd := a.agentTab.Update(msg)
	a.agentTab = updated
	return a, cmd
}

// View implements tea.Model.
func (a App) View() tea.View {
	// Build bottom section first to calculate height.
	hints := a.agentTab.Hints()
	tabStatusLine := a.agentTab.StatusLine()

	// Bottom: divider + status line + hints (only if non-nil).
	bottomDivider := centerBlockUniform(gradientDivider(chatContentWidth(a.width)), a.width)
	cwdLine := centerBlockUniform(
		lipgloss.NewStyle().Foreground(ColorMuted).Render(cwdShort()),
		a.width,
	)
	var bottomParts []string
	bottomParts = append(bottomParts, bottomDivider)
	bottomParts = append(bottomParts, cwdLine)
	if tabStatusLine != "" {
		bottomParts = append(bottomParts, centerBlockUniform(tabStatusLine, a.width))
	}
	if len(hints) > 0 {
		bottomParts = append(bottomParts, centerBlockUniform(
			components.StatusBarFromItems(hints, a.width), a.width))
	}
	bottom := "\n" + strings.Join(bottomParts, "\n")
	bottomLines := countViewLines(bottom)

	// No fixed top - banner is inside the scrollable viewport.
	contentHeight := a.height - bottomLines - 1
	if contentHeight < 3 {
		contentHeight = 3
	}

	content := a.agentTab.View(a.width, contentHeight)

	v := tea.NewView(content + bottom)
	v.AltScreen = true
	return v
}

// CenterBlockUniform centers a multi-line block within the given width,
// using the widest line to calculate uniform left padding.
func centerBlockUniform(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	maxWidth := 0
	for _, line := range lines {
		w := lipgloss.Width(line)
		if w > maxWidth {
			maxWidth = w
		}
	}
	if maxWidth <= 0 || maxWidth >= width {
		return s
	}
	pad := (width - maxWidth) / 2
	if pad <= 0 {
		return s
	}
	prefix := strings.Repeat(" ", pad)
	for i := range lines {
		if lines[i] != "" {
			lines[i] = prefix + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// CountViewLines counts the number of visible lines in a rendered block.
func countViewLines(block string) int {
	if strings.TrimSpace(block) == "" {
		return 0
	}
	return strings.Count(block, "\n") + 1
}
