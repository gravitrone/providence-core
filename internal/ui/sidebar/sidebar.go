package sidebar

import (
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/harmonica"
)

// EvictGracePeriod is how long a completed agent lingers before sliding out.
const EvictGracePeriod = 30 * time.Second

// Model holds the sidebar state: agent cards, focus, and spring animation.
type Model struct {
	Agents    []AgentCard
	Focused   bool
	Cursor    int
	Expanded  bool
	Width     int
	Spring    harmonica.Spring
	Position  float64 // 0.0=hidden, 1.0=visible
	PosVel    float64
	EvictTime map[string]time.Time // agent ID -> completion time for 30s eviction
}

// New creates a sidebar model with a critically-damped spring.
func New() Model {
	return Model{
		Spring:    harmonica.NewSpring(harmonica.FPS(12), 6.0, 0.8),
		EvictTime: make(map[string]time.Time),
	}
}

// Update syncs the sidebar from runner state. Call each tick.
func (m *Model) Update(agents []AgentCard) {
	m.Agents = agents

	// Track eviction times for completed/failed/killed agents.
	activeIDs := make(map[string]bool, len(agents))
	for _, a := range agents {
		activeIDs[a.ID] = true
		if a.Status == "completed" || a.Status == "failed" || a.Status == "killed" {
			if _, ok := m.EvictTime[a.ID]; !ok {
				m.EvictTime[a.ID] = time.Now()
			}
		} else {
			// Agent was reset or re-running, clear eviction.
			delete(m.EvictTime, a.ID)
		}
	}

	// Clean up eviction entries for agents that no longer exist.
	for id := range m.EvictTime {
		if !activeIDs[id] {
			delete(m.EvictTime, id)
		}
	}
}

// HasAgents returns true if the sidebar should be visible. Includes the
// 30-second grace period after all agents complete.
func (m *Model) HasAgents() bool {
	if len(m.Agents) == 0 {
		return false
	}
	for _, a := range m.Agents {
		if a.Status == "running" {
			return true
		}
	}
	// All agents are done. Check if any are within grace period.
	for _, a := range m.Agents {
		if et, ok := m.EvictTime[a.ID]; ok {
			if time.Since(et) < EvictGracePeriod {
				return true
			}
		}
	}
	return false
}

// Tick updates the spring position and clamps. Call from the flame tick handler.
func (m *Model) Tick() {
	target := 0.0
	if m.HasAgents() {
		target = 1.0
	}
	m.Position, m.PosVel = m.Spring.Update(m.Position, m.PosVel, target)

	// Clamp.
	if m.Position < 0.001 {
		m.Position = 0.0
		m.PosVel = 0.0
	}
	if m.Position > 0.999 {
		m.Position = 1.0
	}
}

// EffectiveWidth returns the animated sidebar width based on spring position.
func (m *Model) EffectiveWidth(maxWidth int) int {
	if m.Position <= 0.0 {
		return 0
	}
	w := int(math.Round(float64(maxWidth) * m.Position))
	if w < 0 {
		w = 0
	}
	if w > maxWidth {
		w = maxWidth
	}
	return w
}

// VisibleAgents returns agents that haven't been evicted yet.
func (m *Model) VisibleAgents() []AgentCard {
	now := time.Now()
	out := make([]AgentCard, 0, len(m.Agents))
	for _, a := range m.Agents {
		if et, ok := m.EvictTime[a.ID]; ok {
			if now.Sub(et) >= EvictGracePeriod {
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

// View renders the full sidebar panel within the given width and height.
func (m *Model) View(width, height, flameFrame int) string {
	if width < 4 || height < 3 {
		return ""
	}

	visible := m.VisibleAgents()

	// Title line.
	titleText := " Agents "
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(themePrimary))
	title := titleStyle.Render(titleText)

	// Build border top: ┌─ Agents ─────┐
	innerW := width - 2 // minus left+right border chars
	titleW := lipgloss.Width(title)
	dashesLeft := 1
	dashesRight := innerW - dashesLeft - titleW
	if dashesRight < 0 {
		dashesRight = 0
	}
	borderColor := lipgloss.Color(themeBorder)
	bc := lipgloss.NewStyle().Foreground(borderColor)
	topLine := bc.Render("┌") +
		bc.Render(strings.Repeat("─", dashesLeft)) +
		title +
		bc.Render(strings.Repeat("─", dashesRight)) +
		bc.Render("┐")

	// Bottom border.
	bottomLine := bc.Render("└") +
		bc.Render(strings.Repeat("─", innerW)) +
		bc.Render("┘")

	// Render agent cards in the body area.
	bodyH := height - 2 // minus top and bottom border lines
	if bodyH < 1 {
		bodyH = 1
	}

	var bodyLines []string
	for i, agent := range visible {
		selected := m.Focused && i == m.Cursor
		cardLines := RenderAgentCard(agent, innerW, selected, flameFrame)
		bodyLines = append(bodyLines, cardLines...)
		// Add a blank separator between cards (not after the last one).
		if i < len(visible)-1 {
			bodyLines = append(bodyLines, "")
		}
	}

	// If no agents, show placeholder.
	if len(visible) == 0 {
		placeholder := lipgloss.NewStyle().
			Foreground(lipgloss.Color(themeMuted)).
			Italic(true).
			Render("no agents")
		bodyLines = []string{placeholder}
	}

	// Pad or truncate to fit bodyH.
	for len(bodyLines) < bodyH {
		bodyLines = append(bodyLines, "")
	}
	if len(bodyLines) > bodyH {
		bodyLines = bodyLines[:bodyH]
	}

	// Wrap each line with side borders and pad to innerW.
	var out strings.Builder
	out.WriteString(topLine + "\n")
	for _, line := range bodyLines {
		lineW := lipgloss.Width(line)
		pad := innerW - lineW
		if pad < 0 {
			pad = 0
			// Truncate line to innerW.
			line = truncateToWidth(line, innerW)
		}
		out.WriteString(bc.Render("│") + " " + line + strings.Repeat(" ", pad-1) + bc.Render("│") + "\n")
	}
	out.WriteString(bottomLine)

	return out.String()
}

// truncateToWidth naively truncates a string to fit within w visible characters.
func truncateToWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	// Use lipgloss width for ANSI-aware truncation.
	if lipgloss.Width(s) <= w {
		return s
	}
	// Fallback: strip rune by rune (not perfect with ANSI but safe).
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > w {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

// StatusIcon returns the appropriate icon for an agent's status.
func StatusIcon(status string, frame int) string {
	switch status {
	case "running":
		// Pulse between bright and dim.
		if (frame/3)%2 == 0 {
			return lipgloss.NewStyle().Foreground(lipgloss.Color(themePrimary)).Bold(true).Render("●")
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color(themeMuted)).Render("●")
	case "completed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(themeSuccess)).Render("✓")
	case "failed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(themeError)).Render("×")
	case "killed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(themeError)).Render("×")
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(themeMuted)).Render("◇")
	}
}

// FormatElapsed returns a compact elapsed duration string.
func FormatElapsed(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", h, m)
}
