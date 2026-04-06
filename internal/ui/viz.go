package ui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/list"
	"charm.land/lipgloss/v2/table"
	"charm.land/lipgloss/v2/tree"
)

// vizBlockRe matches ```providence-viz ... ``` fenced code blocks.
// Captures the JSON content between the fences.
var vizBlockRe = regexp.MustCompile("(?s)```providence-viz\\s*\n(.*?)```")

// VizData is the universal envelope for all visualization types.
type VizData struct {
	Type    string          `json:"type"`
	Title   string          `json:"title,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Headers []string        `json:"headers,omitempty"`
	Rows    [][]string      `json:"rows,omitempty"`
	Root    *TreeNode       `json:"root,omitempty"`
	Items   []string        `json:"items,omitempty"`
	// Fields for new viz types.
	Value    float64  `json:"value,omitempty"`
	Max      float64  `json:"max,omitempty"`
	Unit     string   `json:"unit,omitempty"`
	Delta    string   `json:"delta,omitempty"`
	Label    string   `json:"label,omitempty"`
	Events   []Event  `json:"events,omitempty"`
	Entries  []KVPair `json:"entries,omitempty"`
	OldLines []string `json:"old_lines,omitempty"`
	NewLines []string `json:"new_lines,omitempty"`
}

// BarItem is a single bar in a bar chart.
type BarItem struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

// Event is a single entry in a timeline visualization.
type Event struct {
	Time  string `json:"time"`
	Label string `json:"label"`
}

// KVPair is a key-value entry.
type KVPair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// TreeNode is a recursive node in a tree visualization.
type TreeNode struct {
	Name     string      `json:"name"`
	Children []*TreeNode `json:"children,omitempty"`
}

// vizTitleStyle renders the visualization title.
var vizTitleStyle = lipgloss.NewStyle().
	Foreground(ColorAccent).
	Bold(true).
	Underline(true).
	MarginBottom(1)

// ProcessVizBlocks finds all ```providence-viz blocks in content,
// renders them, and replaces them with the rendered output.
// Returns the processed content string.
func ProcessVizBlocks(content string, width int) string {
	return vizBlockRe.ReplaceAllStringFunc(content, func(match string) string {
		subs := vizBlockRe.FindStringSubmatch(match)
		if len(subs) < 2 {
			return match
		}
		rendered := RenderVisualization(strings.TrimSpace(subs[1]), width)
		if rendered == "" {
			return match // failed to render, leave the raw block
		}
		return rendered
	})
}

// ExtractAndRenderVizBlocks replaces viz blocks with unique placeholders,
// renders each viz, and returns the modified content + a map of placeholder -> rendered output.
// This allows glamour to process the markdown without mangling ANSI codes in viz output.
func ExtractAndRenderVizBlocks(content string, width int) (string, map[string]string) {
	vizMap := make(map[string]string)
	idx := 0
	result := vizBlockRe.ReplaceAllStringFunc(content, func(match string) string {
		subs := vizBlockRe.FindStringSubmatch(match)
		if len(subs) < 2 {
			return match
		}
		rendered := RenderVisualization(strings.TrimSpace(subs[1]), width)
		if rendered == "" {
			return match
		}
		placeholder := fmt.Sprintf("PROVIDENCEVIZBLOCK%d", idx)
		idx++
		vizMap[placeholder] = rendered
		return placeholder
	})
	return result, vizMap
}

// RenderVisualization parses vizJSON and dispatches to the correct renderer.
// Returns empty string on any parse/render failure.
func RenderVisualization(vizJSON string, width int) string {
	var v VizData
	if err := json.Unmarshal([]byte(vizJSON), &v); err != nil {
		return ""
	}

	// Clamp width to something reasonable.
	if width <= 0 {
		width = 80
	}
	if width > 120 {
		width = 120
	}

	var body string
	switch v.Type {
	case "bar":
		body = renderBarChart(v, width)
	case "table":
		body = renderTable(v, width)
	case "sparkline":
		body = renderSparkline(v, width)
	case "tree":
		body = renderTree(v)
	case "list":
		body = renderList(v)
	case "progress":
		body = renderProgress(v, width)
	case "gauge":
		body = renderGauge(v, width)
	case "heatmap":
		body = renderHeatmap(v, width)
	case "timeline":
		body = renderTimeline(v)
	case "kv":
		body = renderKV(v)
	case "stat":
		body = renderStat(v)
	case "diff":
		body = renderDiff(v)
	default:
		return ""
	}

	if body == "" {
		return ""
	}

	var b strings.Builder
	if v.Title != "" {
		b.WriteString(vizTitleStyle.Render(v.Title))
		b.WriteString("\n")
	}
	b.WriteString(body)
	b.WriteString("\n")
	return b.String()
}

// renderBarChart renders horizontal bars with block chars.
func renderBarChart(v VizData, width int) string {
	var items []BarItem
	if err := json.Unmarshal(v.Data, &items); err != nil {
		return ""
	}
	if len(items) == 0 {
		return ""
	}

	// Find max value and longest label for alignment.
	maxVal := 0.0
	maxLabel := 0
	for _, item := range items {
		if item.Value > maxVal {
			maxVal = item.Value
		}
		if len(item.Label) > maxLabel {
			maxLabel = len(item.Label)
		}
	}
	if maxVal == 0 {
		maxVal = 1
	}

	// Reserve space: label + " " + bar + " " + value
	// Value display is max ~7 chars (e.g. " 100.0")
	valueWidth := 7
	barMaxWidth := width - maxLabel - valueWidth - 3 // padding
	if barMaxWidth < 10 {
		barMaxWidth = 10
	}

	labelStyle := lipgloss.NewStyle().Foreground(ColorText).Width(maxLabel).Align(lipgloss.Right)
	valueStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	// Flame gradient: items cycle through primary (amber), secondary (flame orange), accent (gold).
	colors := []color.Color{ColorPrimary, ColorSecondary, ColorAccent}

	var b strings.Builder
	for i, item := range items {
		barLen := int(math.Round((item.Value / maxVal) * float64(barMaxWidth)))
		if barLen < 1 && item.Value > 0 {
			barLen = 1
		}

		barColor := colors[i%len(colors)]
		barStyle := lipgloss.NewStyle().Foreground(barColor)

		bar := barStyle.Render(strings.Repeat("█", barLen))
		label := labelStyle.Render(item.Label)
		val := valueStyle.Render(fmt.Sprintf(" %.0f", item.Value))

		b.WriteString(label + " " + bar + val + "\n")
	}
	return b.String()
}

// renderTable renders a styled table with rounded borders and flame theme.
func renderTable(v VizData, width int) string {
	if len(v.Headers) == 0 && len(v.Rows) == 0 {
		return ""
	}

	headerStyle := lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Bold(true).
		Align(lipgloss.Center).
		Padding(0, 1)

	cellStyle := lipgloss.NewStyle().Padding(0, 1)
	oddStyle := cellStyle.Foreground(ColorText)
	evenStyle := cellStyle.Foreground(ColorMuted)

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(ColorBorder)).
		StyleFunc(func(row, col int) lipgloss.Style {
			switch {
			case row == table.HeaderRow:
				return headerStyle
			case row%2 == 0:
				return evenStyle
			default:
				return oddStyle
			}
		}).
		Width(width)

	if len(v.Headers) > 0 {
		t = t.Headers(v.Headers...)
	}
	if len(v.Rows) > 0 {
		t = t.Rows(v.Rows...)
	}

	return t.String()
}

// sparkBlocks maps normalized values (0-7) to block characters.
var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// sparkColors is the flame gradient for sparkline rendering.
var sparkColors = []color.Color{
	lipgloss.Color("#6b5040"), // low - ember ash
	lipgloss.Color("#D77757"), // mid - flame orange
	lipgloss.Color("#FFA600"), // high - profaned amber
	lipgloss.Color("#FFD700"), // peak - holy gold
}

// renderSparkline renders an inline sparkline using block characters with flame gradient.
func renderSparkline(v VizData, _ int) string {
	var values []float64
	if err := json.Unmarshal(v.Data, &values); err != nil {
		return ""
	}
	if len(values) == 0 {
		return ""
	}

	minVal, maxVal := values[0], values[0]
	for _, val := range values {
		if val < minVal {
			minVal = val
		}
		if val > maxVal {
			maxVal = val
		}
	}

	valRange := maxVal - minVal
	if valRange == 0 {
		valRange = 1
	}

	var b strings.Builder
	for _, val := range values {
		// Normalize to 0-7 range for block selection.
		norm := (val - minVal) / valRange
		idx := int(math.Round(norm * 7))
		if idx > 7 {
			idx = 7
		}

		// Pick color from flame gradient based on normalized value.
		colorIdx := int(math.Round(norm * float64(len(sparkColors)-1)))
		if colorIdx >= len(sparkColors) {
			colorIdx = len(sparkColors) - 1
		}

		style := lipgloss.NewStyle().Foreground(sparkColors[colorIdx])
		b.WriteString(style.Render(string(sparkBlocks[idx])))
	}

	// Add min/max annotation.
	annotStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	b.WriteString(annotStyle.Render(fmt.Sprintf("  %.0f-%.0f", minVal, maxVal)))

	return b.String()
}

// renderTree renders a tree visualization using lipgloss/tree.
func renderTree(v VizData) string {
	if v.Root == nil {
		return ""
	}

	enumStyle := lipgloss.NewStyle().Foreground(ColorSecondary).MarginRight(1)
	rootStyle := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	itemStyle := lipgloss.NewStyle().Foreground(ColorText)

	t := tree.Root(v.Root.Name).
		Enumerator(tree.RoundedEnumerator).
		EnumeratorStyle(enumStyle).
		RootStyle(rootStyle).
		ItemStyle(itemStyle)

	addTreeChildren(t, v.Root.Children)
	return t.String()
}

// addTreeChildren recursively adds children to a lipgloss tree.
func addTreeChildren(t *tree.Tree, children []*TreeNode) {
	for _, child := range children {
		if len(child.Children) > 0 {
			subtree := tree.Root(child.Name)
			addTreeChildren(subtree, child.Children)
			t.Child(subtree)
		} else {
			t.Child(child.Name)
		}
	}
}

// renderProgress renders a percentage progress bar.
func renderProgress(v VizData, width int) string {
	maxVal := v.Max
	if maxVal == 0 {
		maxVal = 100
	}
	pct := v.Value / maxVal
	if pct > 1 {
		pct = 1
	}
	if pct < 0 {
		pct = 0
	}

	label := v.Label
	if label == "" {
		label = "Progress"
	}

	// Bar width: total - label - " " - " " - "100%" - padding
	barWidth := width - len(label) - 10
	if barWidth < 10 {
		barWidth = 10
	}

	filled := int(math.Round(pct * float64(barWidth)))
	empty := barWidth - filled

	// Color shifts from ember (low) to gold (high)
	var barColor color.Color
	switch {
	case pct < 0.3:
		barColor = lipgloss.Color("#6b5040")
	case pct < 0.6:
		barColor = ColorSecondary
	case pct < 0.9:
		barColor = ColorPrimary
	default:
		barColor = ColorAccent
	}

	filledStyle := lipgloss.NewStyle().Foreground(barColor)
	emptyStyle := lipgloss.NewStyle().Foreground(ColorBorder)
	labelStyle := lipgloss.NewStyle().Foreground(ColorText)
	pctStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	bar := filledStyle.Render(strings.Repeat("█", filled)) + emptyStyle.Render(strings.Repeat("░", empty))
	return labelStyle.Render(label) + " " + bar + " " + pctStyle.Render(fmt.Sprintf("%.0f%%", pct*100)) + "\n"
}

// renderGauge renders a meter-style gauge for a single value.
func renderGauge(v VizData, width int) string {
	maxVal := v.Max
	if maxVal == 0 {
		maxVal = 100
	}
	pct := v.Value / maxVal
	if pct > 1 {
		pct = 1
	}

	unit := v.Unit
	label := v.Label
	if label == "" {
		label = "Value"
	}

	// Build gauge: [████████░░░░] 75/100 unit
	gaugeWidth := width/2 - 4
	if gaugeWidth < 10 {
		gaugeWidth = 10
	}

	filled := int(math.Round(pct * float64(gaugeWidth)))
	empty := gaugeWidth - filled

	// Color based on level
	var barColor color.Color
	switch {
	case pct < 0.5:
		barColor = ColorSuccess
	case pct < 0.8:
		barColor = ColorPrimary
	default:
		barColor = lipgloss.Color("#FF4444") // danger red
	}

	filledStyle := lipgloss.NewStyle().Foreground(barColor)
	emptyStyle := lipgloss.NewStyle().Foreground(ColorBorder)
	labelStyle := lipgloss.NewStyle().Foreground(ColorText).Bold(true)
	valueStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	bar := "[" + filledStyle.Render(strings.Repeat("█", filled)) + emptyStyle.Render(strings.Repeat("░", empty)) + "]"
	valStr := fmt.Sprintf("%.0f/%.0f", v.Value, maxVal)
	if unit != "" {
		valStr += " " + unit
	}

	return labelStyle.Render(label) + "\n" + bar + " " + valueStyle.Render(valStr) + "\n"
}

// renderHeatmap renders a grid with color intensity.
func renderHeatmap(v VizData, _ int) string {
	// Data is [][]float64 (rows of values)
	var grid [][]float64
	if err := json.Unmarshal(v.Data, &grid); err != nil {
		return ""
	}
	if len(grid) == 0 {
		return ""
	}

	// Find min/max across all cells
	minVal, maxVal := grid[0][0], grid[0][0]
	for _, row := range grid {
		for _, val := range row {
			if val < minVal {
				minVal = val
			}
			if val > maxVal {
				maxVal = val
			}
		}
	}
	valRange := maxVal - minVal
	if valRange == 0 {
		valRange = 1
	}

	// Heatmap gradient: dark ember -> flame -> gold -> white-hot
	heatColors := []color.Color{
		lipgloss.Color("#2a1810"),
		lipgloss.Color("#4c2210"),
		lipgloss.Color("#6b5040"),
		lipgloss.Color("#D77757"),
		lipgloss.Color("#FFA600"),
		lipgloss.Color("#FFD700"),
		lipgloss.Color("#FFEC80"),
	}

	var b strings.Builder
	// Column headers if available
	if len(v.Headers) > 0 {
		headerStyle := lipgloss.NewStyle().Foreground(ColorMuted)
		b.WriteString("   ")
		for _, h := range v.Headers {
			b.WriteString(headerStyle.Render(fmt.Sprintf("%-3s", h)))
		}
		b.WriteString("\n")
	}

	for i, row := range grid {
		// Row label
		rowLabel := fmt.Sprintf("%d", i)
		if i < len(v.Items) {
			rowLabel = v.Items[i]
		}
		labelStyle := lipgloss.NewStyle().Foreground(ColorMuted).Width(3)
		b.WriteString(labelStyle.Render(rowLabel))

		for _, val := range row {
			norm := (val - minVal) / valRange
			colorIdx := int(math.Round(norm * float64(len(heatColors)-1)))
			if colorIdx >= len(heatColors) {
				colorIdx = len(heatColors) - 1
			}
			style := lipgloss.NewStyle().Foreground(heatColors[colorIdx])
			b.WriteString(style.Render("██ "))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderTimeline renders chronological events.
func renderTimeline(v VizData) string {
	if len(v.Events) == 0 {
		return ""
	}

	timeStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	lineStyle := lipgloss.NewStyle().Foreground(ColorBorder)
	dotStyle := lipgloss.NewStyle().Foreground(ColorPrimary)
	textStyle := lipgloss.NewStyle().Foreground(ColorText)

	var b strings.Builder
	for i, event := range v.Events {
		b.WriteString(timeStyle.Render(event.Time))
		b.WriteString(" " + dotStyle.Render("●") + " ")
		b.WriteString(textStyle.Render(event.Label))
		b.WriteString("\n")
		if i < len(v.Events)-1 {
			padding := strings.Repeat(" ", len(event.Time)+1)
			b.WriteString(padding + lineStyle.Render("│") + "\n")
		}
	}
	return b.String()
}

// renderKV renders styled key-value pairs.
func renderKV(v VizData) string {
	if len(v.Entries) == 0 {
		return ""
	}

	// Find max key width for alignment
	maxKey := 0
	for _, e := range v.Entries {
		if len(e.Key) > maxKey {
			maxKey = len(e.Key)
		}
	}

	keyStyle := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Width(maxKey).Align(lipgloss.Right)
	sepStyle := lipgloss.NewStyle().Foreground(ColorBorder)
	valStyle := lipgloss.NewStyle().Foreground(ColorText)

	var b strings.Builder
	for _, e := range v.Entries {
		b.WriteString(keyStyle.Render(e.Key))
		b.WriteString(sepStyle.Render("  :  "))
		b.WriteString(valStyle.Render(e.Value))
		b.WriteString("\n")
	}
	return b.String()
}

// renderStat renders a big number card with label and optional delta.
func renderStat(v VizData) string {
	label := v.Label
	if label == "" {
		label = "Value"
	}
	unit := v.Unit

	valueStr := fmt.Sprintf("%.0f", v.Value)
	if unit != "" {
		valueStr += " " + unit
	}

	bigStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	var b strings.Builder
	b.WriteString(labelStyle.Render(label) + "\n")
	b.WriteString(bigStyle.Render(valueStr) + "\n")

	if v.Delta != "" {
		var deltaColor color.Color
		if strings.HasPrefix(v.Delta, "+") || strings.HasPrefix(v.Delta, "▲") {
			deltaColor = ColorSuccess
		} else if strings.HasPrefix(v.Delta, "-") || strings.HasPrefix(v.Delta, "▼") {
			deltaColor = lipgloss.Color("#FF4444")
		} else {
			deltaColor = ColorMuted
		}
		deltaStyle := lipgloss.NewStyle().Foreground(deltaColor)
		b.WriteString(deltaStyle.Render(v.Delta) + "\n")
	}
	return b.String()
}

// renderDiff renders a colorized inline diff view.
func renderDiff(v VizData) string {
	if len(v.OldLines) == 0 && len(v.NewLines) == 0 {
		return ""
	}

	removeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4444"))
	addStyle := lipgloss.NewStyle().Foreground(ColorSuccess)
	contextStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	var b strings.Builder

	// Show removed lines
	for _, line := range v.OldLines {
		b.WriteString(removeStyle.Render("- " + line) + "\n")
	}
	// Show added lines
	for _, line := range v.NewLines {
		b.WriteString(addStyle.Render("+ " + line) + "\n")
	}

	// If both exist, show summary
	if len(v.OldLines) > 0 && len(v.NewLines) > 0 {
		summary := fmt.Sprintf("%d removed, %d added", len(v.OldLines), len(v.NewLines))
		b.WriteString(contextStyle.Render(summary) + "\n")
	}
	return b.String()
}

// renderList renders a styled bullet list using lipgloss/list.
func renderList(v VizData) string {
	// Try items field first, fall back to data field.
	items := v.Items
	if len(items) == 0 {
		if err := json.Unmarshal(v.Data, &items); err != nil {
			return ""
		}
	}
	if len(items) == 0 {
		return ""
	}

	enumStyle := lipgloss.NewStyle().Foreground(ColorSecondary).MarginRight(1)
	itemStyle := lipgloss.NewStyle().Foreground(ColorText)

	// Convert to []any for list.New.
	anyItems := make([]any, len(items))
	for i, item := range items {
		anyItems[i] = item
	}

	l := list.New(anyItems...).
		EnumeratorStyle(enumStyle).
		ItemStyle(itemStyle)

	return l.String()
}
