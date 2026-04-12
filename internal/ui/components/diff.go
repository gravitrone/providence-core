package components

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	diffDefaultVisibleLines = 10
	diffWordThreshold       = 0.40 // max difference ratio for word-level highlighting
)

// DiffTheme holds flame-themed colors for diff rendering.
type DiffTheme struct {
	AdditionFg  string // holy gold text
	AdditionBg  string // subtle gold bg
	DeletionFg  string // light text on brimstone
	DeletionBg  string // brimstone red bg
	ContextFg   string // muted ash
	HunkFg      string // ember glow
	LineNumLow  string // darker line numbers at top
	LineNumHigh string // brighter line numbers at bottom
	WordAddFg   string // brighter amber for changed words
	WordDelFg   string // deeper ember for changed words
}

// DefaultDiffTheme returns the flame-themed diff colors.
func DefaultDiffTheme() DiffTheme {
	return DiffTheme{
		AdditionFg:  "#FFD700",
		AdditionBg:  "#2a2000",
		DeletionFg:  "#e0b0b0",
		DeletionBg:  "#3a1010",
		ContextFg:   "#3D3530",
		HunkFg:      "#D77757",
		LineNumLow:  "#3D3530",
		LineNumHigh: "#D77757",
		WordAddFg:   "#FFE44D",
		WordDelFg:   "#8B1A1A",
	}
}

// DiffLine represents a single line in a rendered diff.
type DiffLine struct {
	Type    DiffLineType
	Content string
	OldNum  int
	NewNum  int
}

// DiffLineType identifies what kind of diff line this is.
type DiffLineType int

const (
	// DiffContext is an unchanged context line.
	DiffContext DiffLineType = iota
	// DiffAdd is an added line.
	DiffAdd
	// DiffDel is a deleted line.
	DiffDel
	// DiffHunk is a hunk header (@@ ... @@).
	DiffHunk
)

// ComputeDiffLines computes a simple line-by-line diff between old and new content.
// Returns a list of DiffLine entries for rendering.
func ComputeDiffLines(oldContent, newContent string) []DiffLine {
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)

	// Simple LCS-based diff.
	lcs := lcsTable(oldLines, newLines)
	return buildDiffFromLCS(oldLines, newLines, lcs)
}

// RenderDiff renders a unified diff with flame theme colors.
func RenderDiff(oldContent, newContent, filename string, width int, theme DiffTheme) string {
	if width <= 0 {
		width = 80
	}

	lines := ComputeDiffLines(oldContent, newContent)
	if len(lines) == 0 {
		return ""
	}

	// Styles.
	addStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.AdditionFg)).
		Background(lipgloss.Color(theme.AdditionBg))
	delStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.DeletionFg)).
		Background(lipgloss.Color(theme.DeletionBg))
	ctxStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ContextFg))
	hunkStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.HunkFg)).
		Bold(true)

	var b strings.Builder

	// File header.
	if filename != "" {
		headerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.HunkFg)).
			Bold(true)
		b.WriteString(headerStyle.Render("--- "+filename))
		b.WriteString("\n")
		b.WriteString(headerStyle.Render("+++ "+filename))
		b.WriteString("\n")
	}

	totalLines := len(lines)
	visibleCount := diffDefaultVisibleLines
	collapsed := totalLines > visibleCount

	renderCount := totalLines
	if collapsed {
		renderCount = visibleCount
	}

	for i := 0; i < renderCount; i++ {
		dl := lines[i]
		lineNum := renderLineNum(i, totalLines, theme)
		content := clampDiffLine(dl.Content, width-8)

		switch dl.Type {
		case DiffAdd:
			b.WriteString(lineNum + addStyle.Render("+ "+content))
		case DiffDel:
			b.WriteString(lineNum + delStyle.Render("- "+content))
		case DiffHunk:
			b.WriteString(hunkStyle.Render(content))
		default:
			b.WriteString(lineNum + ctxStyle.Render("  "+content))
		}
		b.WriteString("\n")
	}

	if collapsed {
		remaining := totalLines - visibleCount
		expandStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.HunkFg)).
			Italic(true)
		b.WriteString(expandStyle.Render(fmt.Sprintf("  ... show %d more lines", remaining)))
		b.WriteString("\n")
	}

	return b.String()
}

// RenderDiffExpanded renders the full diff without collapsing.
func RenderDiffExpanded(oldContent, newContent, filename string, width int, theme DiffTheme) string {
	if width <= 0 {
		width = 80
	}

	lines := ComputeDiffLines(oldContent, newContent)
	if len(lines) == 0 {
		return ""
	}

	addStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.AdditionFg)).
		Background(lipgloss.Color(theme.AdditionBg))
	delStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.DeletionFg)).
		Background(lipgloss.Color(theme.DeletionBg))
	ctxStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ContextFg))
	hunkStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.HunkFg)).
		Bold(true)

	var b strings.Builder

	if filename != "" {
		headerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.HunkFg)).
			Bold(true)
		b.WriteString(headerStyle.Render("--- "+filename))
		b.WriteString("\n")
		b.WriteString(headerStyle.Render("+++ "+filename))
		b.WriteString("\n")
	}

	for i, dl := range lines {
		lineNum := renderLineNum(i, len(lines), theme)
		content := clampDiffLine(dl.Content, width-8)

		switch dl.Type {
		case DiffAdd:
			b.WriteString(lineNum + addStyle.Render("+ "+content))
		case DiffDel:
			b.WriteString(lineNum + delStyle.Render("- "+content))
		case DiffHunk:
			b.WriteString(hunkStyle.Render(content))
		default:
			b.WriteString(lineNum + ctxStyle.Render("  "+content))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// renderLineNum renders a line number with flame gradient (darker at top, brighter at bottom).
func renderLineNum(idx, total int, theme DiffTheme) string {
	// Interpolate between low and high colors based on position.
	t := 0.0
	if total > 1 {
		t = float64(idx) / float64(total-1)
	}
	color := lerpHexSimple(theme.LineNumLow, theme.LineNumHigh, t)
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	return style.Render(fmt.Sprintf("%4d ", idx+1))
}

// lerpHexSimple does a simple linear interpolation between two hex colors.
// Both colors must be 7-char hex (#RRGGBB). Falls back to low on parse error.
func lerpHexSimple(low, high string, t float64) string {
	r1, g1, b1, ok1 := parseHex(low)
	r2, g2, b2, ok2 := parseHex(high)
	if !ok1 || !ok2 {
		return low
	}
	r := int(float64(r1) + t*float64(r2-r1))
	g := int(float64(g1) + t*float64(g2-g1))
	bl := int(float64(b1) + t*float64(b2-b1))
	return fmt.Sprintf("#%02X%02X%02X", clampByte(r), clampByte(g), clampByte(bl))
}

func parseHex(hex string) (r, g, b int, ok bool) {
	if len(hex) != 7 || hex[0] != '#' {
		return 0, 0, 0, false
	}
	_, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b)
	return r, g, b, err == nil
}

func clampByte(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

func clampDiffLine(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	// Trim trailing empty line from final newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// lcsTable builds the LCS length table for two string slices.
func lcsTable(a, b []string) [][]int {
	m := len(a)
	n := len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	return dp
}

// buildDiffFromLCS walks the LCS table to produce diff lines.
func buildDiffFromLCS(oldLines, newLines []string, dp [][]int) []DiffLine {
	var result []DiffLine
	i := len(oldLines)
	j := len(newLines)

	// Build in reverse, then flip.
	var rev []DiffLine
	oldNum := i
	newNum := j

	for i > 0 || j > 0 {
		if i > 0 && j > 0 && oldLines[i-1] == newLines[j-1] {
			rev = append(rev, DiffLine{
				Type:    DiffContext,
				Content: oldLines[i-1],
				OldNum:  oldNum,
				NewNum:  newNum,
			})
			i--
			j--
			oldNum--
			newNum--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			rev = append(rev, DiffLine{
				Type:    DiffAdd,
				Content: newLines[j-1],
				NewNum:  newNum,
			})
			j--
			newNum--
		} else if i > 0 {
			rev = append(rev, DiffLine{
				Type:    DiffDel,
				Content: oldLines[i-1],
				OldNum:  oldNum,
			})
			i--
			oldNum--
		}
	}

	// Reverse to get correct order.
	for k := len(rev) - 1; k >= 0; k-- {
		result = append(result, rev[k])
	}

	return result
}
