package components

import "charm.land/lipgloss/v2"

// Exported style vars so the theme system can update them.
var (
	HintDescStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6b5040"))
	KeyCapStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#0a0a0a")).
			Background(lipgloss.Color("#D77757")).
			Bold(true).
			Padding(0, 1)
	SegmentStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#3a2a1a")).
			Padding(0, 1).
			MarginRight(1)
	StatusBarBorder = lipgloss.NewStyle()
)

// HintItem represents a single keybind hint with a key and description.
type HintItem struct {
	Key  string
	Desc string
}

// StatusBarFromItems renders a StatusBar from a slice of HintItem.
func StatusBarFromItems(items []HintItem, width int) string {
	hints := make([]string, len(items))
	for i, item := range items {
		hints[i] = Hint(item.Key, item.Desc)
	}
	return StatusBar(hints, width)
}

// StatusBar renders the bottom hint bar.
func StatusBar(hints []string, width int) string {
	segments := make([]string, 0, len(hints))
	for _, h := range hints {
		segments = append(segments, SegmentStyle.Render(h))
	}
	if width <= 0 {
		content := lipgloss.JoinHorizontal(lipgloss.Top, segments...)
		return StatusBarBorder.Render(content)
	}
	available := width - StatusBarBorder.GetHorizontalFrameSize()
	if available <= 0 {
		available = width
	}
	clamped := clampStatusSegments(segments, available)
	content := lipgloss.JoinHorizontal(lipgloss.Top, clamped...)
	return StatusBarBorder.Width(width).Align(lipgloss.Center).Render(content)
}

// Hint formats a single keybind hint.
func Hint(key, desc string) string {
	keyText := KeyCapStyle.Render(key)
	return HintDescStyle.Render(desc+" ") + keyText
}

func clampStatusSegments(segments []string, width int) []string {
	if len(segments) == 0 || width <= 0 {
		return segments
	}

	out := make([]string, 0, len(segments))
	for _, seg := range segments {
		candidate := append(append([]string{}, out...), seg)
		if statusSegmentsWidth(candidate) > width {
			break
		}
		out = append(out, seg)
	}
	if len(out) == len(segments) {
		return out
	}

	overflow := SegmentStyle.Render(HintDescStyle.Render("More ") + KeyCapStyle.Render("..."))
	for len(out) > 0 && statusSegmentsWidth(append(append([]string{}, out...), overflow)) > width {
		out = out[:len(out)-1]
	}
	if statusSegmentsWidth([]string{overflow}) <= width {
		out = append(out, overflow)
	}
	if len(out) > 0 {
		return out
	}

	return []string{lipgloss.NewStyle().MaxWidth(width).Render(segments[0])}
}

func statusSegmentsWidth(segments []string) int {
	if len(segments) == 0 {
		return 0
	}
	return lipgloss.Width(lipgloss.JoinHorizontal(lipgloss.Top, segments...))
}
