package components

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// AgentStatusState represents the current state of a subagent.
type AgentStatusState string

const (
	AgentRunning    AgentStatusState = "running"
	AgentCompleted  AgentStatusState = "completed"
	AgentFailed     AgentStatusState = "failed"
	AgentKilled     AgentStatusState = "killed"
	AgentBackground AgentStatusState = "background"
)

// Theme hex strings for agent status color math. Updated by the parent theme system.
var (
	AgentThemePrimaryHex = "#D77757"
	AgentThemeMutedHex   = "#6b5040"
)

// AgentStatusInfo describes a single agent's display state.
type AgentStatusInfo struct {
	Name         string
	Model        string
	Status       AgentStatusState
	Elapsed      time.Duration
	LastActivity string
	ParentName   string // empty = top-level agent
	ResultLines  []string
	Expanded     bool
}

// AgentStatusModel is a reusable Bubble Tea component for rendering agent status.
type AgentStatusModel struct {
	Agents []AgentStatusInfo
	Width  int
	Frame  int // animation frame for pulsing
}

// NewAgentStatus creates a new agent status component.
func NewAgentStatus() AgentStatusModel {
	return AgentStatusModel{}
}

// SetAgents updates the agent list.
func (m *AgentStatusModel) SetAgents(agents []AgentStatusInfo) {
	m.Agents = agents
}

// SetFrame updates the animation frame (call from parent tick).
func (m *AgentStatusModel) SetFrame(frame int) {
	m.Frame = frame
}

// View renders the agent status list. No border - caller wraps if needed.
func (m AgentStatusModel) View() string {
	if len(m.Agents) == 0 {
		return ""
	}

	var lines []string

	for _, agent := range m.Agents {
		line := m.renderAgentLine(agent)
		lines = append(lines, line)

		// Activity line with connector.
		if agent.LastActivity != "" {
			activity := m.renderActivityLine(agent)
			lines = append(lines, activity)
		}

		// Expanded result preview (first 3 lines).
		if agent.Expanded && len(agent.ResultLines) > 0 {
			for i, rl := range agent.ResultLines {
				if i >= 3 {
					moreStyle := lipgloss.NewStyle().Foreground(ThemeMuted)
					lines = append(lines, "    "+moreStyle.Render(fmt.Sprintf("...+%d more lines", len(agent.ResultLines)-3)))
					break
				}
				resultStyle := lipgloss.NewStyle().Foreground(ThemeMuted)
				lines = append(lines, "    "+resultStyle.Render(rl))
			}
		}
	}

	return strings.Join(lines, "\n")
}

// renderAgentLine renders the main status line for one agent.
// Format: [indent] icon name    [model]   elapsed
func (m AgentStatusModel) renderAgentLine(agent AgentStatusInfo) string {
	indent := ""
	if agent.ParentName != "" {
		indent = "  "
	}

	icon := m.statusIcon(agent.Status)

	nameStyle := lipgloss.NewStyle().Bold(true).Foreground(ThemeText)
	name := nameStyle.Render(agent.Name)

	modelStyle := lipgloss.NewStyle().Foreground(ThemeMuted)
	model := modelStyle.Render("[" + agent.Model + "]")

	elapsed := FormatDuration(agent.Elapsed)
	elapsedStyle := lipgloss.NewStyle().Foreground(ThemeMuted)
	elapsedStr := elapsedStyle.Render(elapsed)

	contentWidth := m.Width - 4
	if contentWidth < 20 {
		contentWidth = 20
	}

	leftPart := indent + icon + " " + name + " " + model
	leftWidth := lipgloss.Width(leftPart)
	rightWidth := lipgloss.Width(elapsedStr)
	gap := contentWidth - leftWidth - rightWidth
	if gap < 1 {
		gap = 1
	}

	return leftPart + strings.Repeat(" ", gap) + elapsedStr
}

// renderActivityLine renders the indented activity sub-line.
func (m AgentStatusModel) renderActivityLine(agent AgentStatusInfo) string {
	indent := "  "
	if agent.ParentName != "" {
		indent = "    "
	}

	connectorStyle := lipgloss.NewStyle().Foreground(ThemeMuted)
	connector := connectorStyle.Render("\u23BF") // ⎿

	activityStyle := lipgloss.NewStyle().Foreground(ThemeMuted).Italic(true)

	activity := agent.LastActivity
	maxLen := m.Width - 8
	if maxLen < 10 {
		maxLen = 10
	}
	if len(activity) > maxLen {
		activity = activity[:maxLen-3] + "..."
	}

	if agent.Status == AgentRunning {
		activityText := m.renderShimmer(activity)
		return indent + connector + " " + activityText
	}

	return indent + connector + " " + activityStyle.Render(activity)
}

// statusIcon returns the styled icon for the given agent status.
func (m AgentStatusModel) statusIcon(status AgentStatusState) string {
	switch status {
	case AgentRunning:
		pulseC := agentPulseColor(m.Frame, AgentThemePrimaryHex)
		return lipgloss.NewStyle().Foreground(pulseC).Bold(true).Render("\u25CF") // ●
	case AgentCompleted:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#50C878")).Bold(true).Render("\u2713") // ✓
	case AgentFailed, AgentKilled:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#e05050")).Bold(true).Render("\u00D7") // ×
	case AgentBackground:
		return lipgloss.NewStyle().Foreground(ThemeMuted).Render("\u25C7") // ◇
	default:
		return lipgloss.NewStyle().Foreground(ThemeMuted).Render("\u25CF") // ●
	}
}

// renderShimmer applies a simple per-character color shift for running activity text.
func (m AgentStatusModel) renderShimmer(text string) string {
	runes := []rune(text)
	n := len(runes)
	if n == 0 {
		return ""
	}

	offset := (m.Frame * 2) % 60

	var b strings.Builder
	for i, r := range runes {
		t := (math.Sin(float64(i+offset)*0.3) + 1.0) / 2.0
		col := blendHexColor(AgentThemeMutedHex, AgentThemePrimaryHex, t)
		b.WriteString(lipgloss.NewStyle().Foreground(col).Italic(true).Render(string(r)))
	}
	return b.String()
}

// FormatDuration formats a duration into a compact human-readable string.
func FormatDuration(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		if s > 0 {
			return fmt.Sprintf("%dm%ds", m, s)
		}
		return fmt.Sprintf("%dm", m)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", h, m)
}

// agentPulseColor returns a color that breathes between dim and full for the given base hex.
func agentPulseColor(frame int, baseHex string) color.Color {
	t := (math.Sin(float64(frame)*0.14) + 1.0) / 2.0
	dimHex := dimColor(baseHex, 0.4)
	return blendHexColor(dimHex, baseHex, t)
}

// dimColor darkens a hex color by the given factor (0 = black, 1 = original).
func dimColor(hex string, factor float64) string {
	r, g, b := hexToRGBComponents(hex)
	dr := uint8(float64(r) * factor)
	dg := uint8(float64(g) * factor)
	db := uint8(float64(b) * factor)
	return fmt.Sprintf("#%02x%02x%02x", dr, dg, db)
}

// blendHexColor blends two hex color strings and returns a lipgloss-compatible color.Color.
func blendHexColor(a, b string, t float64) color.Color {
	ar, ag, ab := hexToRGBComponents(a)
	br, bg, bb := hexToRGBComponents(b)
	r := uint8(float64(ar) + t*float64(int(br)-int(ar)))
	g := uint8(float64(ag) + t*float64(int(bg)-int(ag)))
	bl := uint8(float64(ab) + t*float64(int(bb)-int(ab)))
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, bl))
}

// hexToRGBComponents parses a hex color to its RGB parts.
func hexToRGBComponents(hex string) (uint8, uint8, uint8) {
	if len(hex) > 0 && hex[0] == '#' {
		hex = hex[1:]
	}
	if len(hex) != 6 {
		return 0, 0, 0
	}
	var r, g, b uint8
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}
