package components

import (
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

var (
	boxBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#3a2a1a")).
			Padding(1, 2)

	boxBorderActive = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#D77757")).
			Padding(1, 2)

	boxMutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6b5040"))

	boxValueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#e0d0c0"))

	boxLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F5A623")).
			Bold(true)

	errorBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7a2f3a")).
			Padding(1, 2)

	errorHeaderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ff5555")).
				Bold(true)

	errorBodyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d6b5b5"))
)

func safeBoxWidth(width int) int {
	if width <= 0 {
		return boxWidth(width)
	}
	w := boxWidth(width)
	if w > width {
		return width
	}
	return w
}

func boxWidth(width int) int {
	if width <= 0 {
		return 0
	}
	w := width - 6
	if w < 40 {
		w = 40
	}
	return w
}

func renderBox(style lipgloss.Style, targetWidth int, content string) string {
	width := safeBoxWidth(targetWidth)
	if width <= 0 {
		return style.Render(content)
	}
	borderW := style.GetBorderLeftSize() + style.GetBorderRightSize()
	inner := width - borderW
	if inner < 1 {
		inner = 1
	}
	return style.Width(inner).Render(content)
}

// Box renders content inside a bordered box.
func Box(content string, width int) string {
	return renderBox(boxBorder, width, content)
}

// BoxContentWidth returns the inner content width excluding border and padding.
func BoxContentWidth(width int) int {
	w := safeBoxWidth(width)
	if w <= 0 {
		return 0
	}
	inner := w - 6
	if inner < 0 {
		return 0
	}
	return inner
}

// ClampTextWidth truncates text to the given visual width.
func ClampTextWidth(text string, width int) string {
	if width <= 0 {
		return text
	}
	cleaned := SanitizeOneLine(text)
	if lipgloss.Width(cleaned) <= width {
		return cleaned
	}
	return truncateRunes(cleaned, width)
}

// ClampTextWidthEllipsis truncates text and adds "..." when truncation occurs.
func ClampTextWidthEllipsis(text string, width int) string {
	if width <= 0 {
		return ""
	}
	cleaned := SanitizeOneLine(text)
	if lipgloss.Width(cleaned) <= width {
		return cleaned
	}
	if width <= 3 {
		return truncateRunes(cleaned, width)
	}
	return truncateRunes(cleaned, width-3) + "..."
}

// ActiveBox renders content inside a highlighted bordered box.
func ActiveBox(content string, width int) string {
	return renderBox(boxBorderActive, width, content)
}

// ErrorBox renders a red bordered box for errors.
func ErrorBox(title, message string, width int) string {
	header := ""
	if title != "" {
		header = errorHeaderStyle.Render(title) + "\n\n"
	}
	body := errorBodyStyle.Render(message)
	return renderBox(errorBorder, width, header+body)
}

// TitledBox renders a box with a header title.
func TitledBox(title, content string, width int) string {
	return renderBox(boxBorder, width, content)
}

// InfoRow renders a label: value row for detail views.
func InfoRow(label, value string) string {
	safeLabel := SanitizeOneLine(label)
	safeValue := SanitizeOneLine(value)
	return boxMutedStyle.Render(safeLabel+": ") + boxValueStyle.Render(safeValue)
}

// TableRow is a single row in a key-value table.
type TableRow struct {
	Label      string
	Value      string
	ValueColor string
}

// Table renders a key-value table with aligned columns inside a bordered box.
func Table(title string, rows []TableRow, width int) string {
	if len(rows) == 0 {
		return ""
	}

	maxLabel := 0
	safeRows := make([]TableRow, len(rows))
	for i, r := range rows {
		safeRows[i] = TableRow{
			Label:      SanitizeOneLine(r.Label),
			Value:      SanitizeOneLine(r.Value),
			ValueColor: r.ValueColor,
		}
		if lipgloss.Width(safeRows[i].Label) > maxLabel {
			maxLabel = lipgloss.Width(safeRows[i].Label)
		}
	}

	contentWidth := BoxContentWidth(width)
	if contentWidth <= 0 {
		contentWidth = maxLabel + 8
	}

	labelWidth := maxLabel
	if labelWidth > 24 {
		labelWidth = 24
	}
	valueWidth := contentWidth - labelWidth - 2
	if valueWidth < 4 {
		valueWidth = 4
	}

	lines := make([]string, 0, len(safeRows))
	for _, r := range safeRows {
		labelText := ClampTextWidth(r.Label, labelWidth)
		label := boxLabelStyle.Render(padRight(labelText, labelWidth))
		valueStyle := boxValueStyle
		if strings.TrimSpace(r.ValueColor) != "" {
			valueStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(r.ValueColor)).Bold(true)
		}
		valueText := ClampTextWidth(r.Value, valueWidth)
		lines = append(lines, label+"  "+valueStyle.Render(valueText))
	}
	content := strings.Join(lines, "\n")

	if title != "" {
		return TitledBox(title, content, width)
	}
	return Box(content, width)
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	var b strings.Builder
	b.Grow(max)
	n := 0
	for _, r := range s {
		if n >= max {
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

func padRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}
