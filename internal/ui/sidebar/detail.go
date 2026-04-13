package sidebar

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// RenderDetail renders the expanded detail view for a single agent.
// Shows the agent header, tool call transcript, and stats.
// scrollOffset controls vertical scroll position; flameFrame drives shimmer.
func RenderDetail(agent AgentCard, width, height, scrollOffset, flameFrame int, colors detailColors) string {
	if width < 12 || height < 5 {
		return ""
	}

	contentW := width - 4 // padding + border

	// --- Header ---

	nameStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colors.PrimaryHex)).
		Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colors.MutedHex))
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colors.TextHex))

	name := agent.Name
	if name == "" {
		name = agent.ID
	}
	header := nameStyle.Render(name)

	// Model + type sub-header.
	var meta []string
	if agent.Model != "" {
		meta = append(meta, agent.Model)
	}
	if agent.Type != "" {
		meta = append(meta, agent.Type)
	}
	metaLine := ""
	if len(meta) > 0 {
		metaLine = mutedStyle.Render(strings.Join(meta, " / "))
	}

	// Status line.
	statusIcon, statusColor := statusIconAndColor(agent.Status, flameFrame, colors)
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor))
	statusLine := statusStyle.Render(statusIcon+" "+agent.Status)

	// --- Tool transcript ---

	var toolLines []string
	for _, tc := range agent.ToolCalls {
		icon := "\u25cf" // ●
		iconColor := colors.MutedHex
		switch tc.Status {
		case "success":
			icon = "\u2713" // ✓
			iconColor = colors.SuccessHex
		case "error":
			icon = "\u00d7" // ×
			iconColor = colors.ErrorHex
		}
		tcIconStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(iconColor))
		toolNameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colors.SecondaryHex)).Bold(true)

		args := tc.Args
		maxArgs := contentW - len(tc.Name) - 6
		if maxArgs < 0 {
			maxArgs = 0
		}
		if len(args) > maxArgs {
			if maxArgs > 3 {
				args = args[:maxArgs-3] + "..."
			} else {
				args = ""
			}
		}

		line := tcIconStyle.Render(icon) + " " + toolNameStyle.Render(tc.Name)
		if args != "" {
			line += " " + mutedStyle.Render(args)
		}
		toolLines = append(toolLines, line)
	}

	if len(toolLines) == 0 {
		toolLines = append(toolLines, mutedStyle.Render("No tool calls yet"))
	}

	// --- Result preview ---

	var resultLines []string
	if agent.Result != "" {
		resultLines = append(resultLines, "")
		resultLines = append(resultLines, textStyle.Render("Result:"))
		// Truncate to 3 lines.
		rLines := strings.Split(agent.Result, "\n")
		if len(rLines) > 3 {
			rLines = rLines[:3]
			rLines = append(rLines, mutedStyle.Render("..."))
		}
		for _, rl := range rLines {
			if len(rl) > contentW {
				rl = rl[:contentW-3] + "..."
			}
			resultLines = append(resultLines, "  "+textStyle.Render(rl))
		}
	}

	// --- Stats footer ---

	elapsed := agent.Completed.Sub(agent.Started)
	if agent.Completed.IsZero() {
		elapsed = 0 // still running, shown differently
	}
	statsStr := fmt.Sprintf("%d tools", agent.ToolCount)
	if elapsed > 0 {
		statsStr += " / " + formatDuration(elapsed)
	}
	if agent.Tokens > 0 {
		statsStr += " / " + formatTokens(agent.Tokens) + " tokens"
	}
	statsLine := mutedStyle.Render(statsStr)

	// --- Assemble all lines ---

	var allLines []string
	allLines = append(allLines, header)
	if metaLine != "" {
		allLines = append(allLines, metaLine)
	}
	allLines = append(allLines, statusLine)
	allLines = append(allLines, "")
	allLines = append(allLines, toolLines...)
	allLines = append(allLines, resultLines...)
	allLines = append(allLines, "")
	allLines = append(allLines, statsLine)
	allLines = append(allLines, "")
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colors.MutedHex))
	allLines = append(allLines, hintStyle.Render("[esc] collapse  [j/k] scroll"))

	// Apply scroll offset.
	if scrollOffset > 0 && scrollOffset < len(allLines) {
		allLines = allLines[scrollOffset:]
	} else if scrollOffset >= len(allLines) {
		allLines = allLines[len(allLines)-1:]
	}

	// Trim to viewport height.
	viewH := height - 2 // border
	if len(allLines) > viewH {
		allLines = allLines[:viewH]
	}

	content := strings.Join(allLines, "\n")

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(colors.BorderHex)).
		Width(width - 2).
		Height(height - 2)

	return borderStyle.Render(content)
}

// detailColors passes theme colors to the detail renderer without coupling
// to the full Sidebar struct.
type detailColors struct {
	PrimaryHex   string
	SecondaryHex string
	MutedHex     string
	TextHex      string
	BorderHex    string
	SuccessHex   string
	ErrorHex     string
}

// DetailColors extracts the color set from a Sidebar for use with RenderDetail.
func (s *Sidebar) DetailColors() detailColors {
	return detailColors{
		PrimaryHex:   s.PrimaryHex,
		SecondaryHex: s.SecondaryHex,
		MutedHex:     s.MutedHex,
		TextHex:      s.TextHex,
		BorderHex:    s.BorderHex,
		SuccessHex:   s.SuccessHex,
		ErrorHex:     s.ErrorHex,
	}
}

// statusIconAndColor returns the display icon and color hex for a status.
func statusIconAndColor(status string, flameFrame int, colors detailColors) (string, string) {
	switch status {
	case "running":
		if flameFrame%4 < 2 {
			return "\u25cf", colors.PrimaryHex
		}
		return "\u25cf", colors.SecondaryHex
	case "completed":
		return "\u2713", colors.SuccessHex
	case "failed":
		return "\u00d7", colors.ErrorHex
	case "killed":
		return "\u00d7", colors.MutedHex
	default:
		return "\u25c7", colors.MutedHex
	}
}
