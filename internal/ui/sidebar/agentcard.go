package sidebar

import (
	"fmt"
	"time"

	"charm.land/lipgloss/v2"
)

// AgentCard holds the display state for a single agent in the sidebar.
type AgentCard struct {
	ID        string
	Name      string
	Status    string // running, completed, failed, killed
	Model     string
	Activity  string // last tool call description
	ToolCount int
	Elapsed   time.Duration
	Started   time.Time
	Result    string // completion result summary
}

// --- Theme Colors (set by UpdateThemeColors) ---

var (
	themePrimary   = "#FFA600"
	themeSecondary = "#D77757"
	themeMuted     = "#6b5040"
	themeText      = "#e0d0c0"
	themeBorder    = "#3a2518"
	themeSuccess   = "#19FA19"
	themeError     = "#ff5555"
	themeCard      = "#1a1210"
)

// UpdateThemeColors sets the sidebar's theme colors. Called from the main
// UI package when the theme changes.
func UpdateThemeColors(primary, secondary, muted, text, border, success, errorC, card string) {
	themePrimary = primary
	themeSecondary = secondary
	themeMuted = muted
	themeText = text
	themeBorder = border
	themeSuccess = success
	themeError = errorC
	themeCard = card
}

// RenderAgentCard renders a single agent card as lines of text that fit within maxW.
func RenderAgentCard(card AgentCard, maxW int, selected bool, frame int) []string {
	if maxW < 6 {
		return nil
	}

	nameStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(themeText)).
		Bold(true)
	activityStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(themeMuted)).
		Italic(true)
	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(themeMuted))

	// If selected, use highlight background.
	if selected {
		bg := lipgloss.Color(themeCard)
		nameStyle = nameStyle.Background(bg)
		activityStyle = activityStyle.Background(bg)
		statsStyle = statsStyle.Background(bg)
	}

	icon := StatusIcon(card.Status, frame)

	// Line 1: icon + name.
	name := card.Name
	if name == "" {
		name = card.ID
	}
	// Truncate name to fit.
	nameMaxW := maxW - 4 // icon + space + margin
	if len(name) > nameMaxW && nameMaxW > 3 {
		name = name[:nameMaxW-1] + "."
	}
	line1 := icon + " " + nameStyle.Render(name)

	// Line 2: activity or result summary.
	var line2 string
	switch card.Status {
	case "running":
		activity := card.Activity
		if activity == "" {
			activity = "working..."
		}
		actMaxW := maxW - 4
		if len(activity) > actMaxW && actMaxW > 3 {
			activity = activity[:actMaxW-1] + "."
		}
		line2 = "  " + activityStyle.Render(activity)
	case "completed":
		result := card.Result
		if result == "" {
			result = "DONE"
		}
		elapsed := FormatElapsed(card.Elapsed)
		summary := fmt.Sprintf("DONE (%s)", elapsed)
		summMaxW := maxW - 4
		if len(summary) > summMaxW && summMaxW > 3 {
			summary = summary[:summMaxW-1] + "."
		}
		line2 = "  " + activityStyle.Render(summary)
		_ = result
	case "failed":
		line2 = "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(themeError)).Render("FAILED")
	case "killed":
		line2 = "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(themeError)).Render("KILLED")
	default:
		line2 = "  " + activityStyle.Render("waiting...")
	}

	// Line 3: stats (tool count + elapsed) - only for running agents.
	var lines []string
	lines = append(lines, line1, line2)

	if card.Status == "running" {
		elapsed := FormatElapsed(card.Elapsed)
		var stats string
		if card.ToolCount > 0 {
			stats = fmt.Sprintf("%d tools, %s", card.ToolCount, elapsed)
		} else {
			stats = elapsed
		}
		line3 := "  " + statsStyle.Render(stats)
		lines = append(lines, line3)
	}

	return lines
}
