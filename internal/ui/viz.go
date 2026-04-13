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
	Value    json.RawMessage `json:"value,omitempty"`
	Max      float64         `json:"max,omitempty"`
	Unit     string          `json:"unit,omitempty"`
	Delta    string          `json:"delta,omitempty"`
	Label    string          `json:"label,omitempty"`
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

func (v VizData) vizValueFloat() float64 {
	if len(v.Value) == 0 {
		return 0
	}
	var f float64
	if err := json.Unmarshal(v.Value, &f); err == nil {
		return f
	}
	return 0
}

func (v VizData) vizValueString() string {
	if len(v.Value) == 0 {
		return "0"
	}
	var f float64
	if err := json.Unmarshal(v.Value, &f); err == nil {
		return fmt.Sprintf("%.0f", f)
	}
	var s string
	if err := json.Unmarshal(v.Value, &s); err == nil {
		return s
	}
	return string(v.Value)
}

// TreeNode is a recursive node in a tree visualization.
type TreeNode struct {
	Name     string      `json:"name"`
	Children []*TreeNode `json:"children,omitempty"`
}

// vizTitleStyle is the base title style; color is overridden dynamically in RenderVisualization.
var vizTitleStyle = lipgloss.NewStyle().
	Bold(true).
	Underline(true).
	MarginBottom(1)

// ProcessVizBlocks replaces all providence-viz fenced blocks in content with rendered output.
func ProcessVizBlocks(content string, width int, frame int) string {
	return vizBlockRe.ReplaceAllStringFunc(content, func(match string) string {
		subs := vizBlockRe.FindStringSubmatch(match)
		if len(subs) < 2 {
			return match
		}
		rendered := RenderVisualization(strings.TrimSpace(subs[1]), width, frame)
		if rendered == "" {
			return match
		}
		return rendered
	})
}

// ExtractAndRenderVizBlocks replaces providence-viz blocks with placeholder tokens and
// returns the modified content and a map from placeholder -> rendered block.
func ExtractAndRenderVizBlocks(content string, width int, frame int) (string, map[string]string) {
	vizMap := make(map[string]string)
	idx := 0
	result := vizBlockRe.ReplaceAllStringFunc(content, func(match string) string {
		subs := vizBlockRe.FindStringSubmatch(match)
		if len(subs) < 2 {
			return match
		}
		rendered := RenderVisualization(strings.TrimSpace(subs[1]), width, frame)
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

// vizBorderGradient is a precomputed gradient for viz block borders.
var vizBorderGradient []color.Color

// vizBarGradient is a precomputed per-character gradient for bar fills.
var vizBarGradient []color.Color

// sparkBlocks maps normalized values (0-7) to block characters.
var sparkBlocks = []rune{'\u2581', '\u2582', '\u2583', '\u2584', '\u2585', '\u2586', '\u2587', '\u2588'}

// sparkColors is the gradient for sparkline rendering.
var sparkColors []color.Color

// recomputeVizGradients rebuilds all viz-related precomputed gradients.
func recomputeVizGradients() {
	vizBorderGradient = lipgloss.Blend1D(20,
		c(ActiveTheme.Border),
		c(ActiveTheme.Muted),
		c(ActiveTheme.Frozen),
		c(ActiveTheme.Muted),
		c(ActiveTheme.Border),
	)

	vizBarGradient = lipgloss.Blend1D(40,
		c(darkenHex(ActiveTheme.Secondary, 0.5)),
		c(ActiveTheme.Secondary),
		c(ActiveTheme.Primary),
		c(ActiveTheme.Accent),
		c(lightenHex(ActiveTheme.Accent, 0.5)),
	)

	sparkColors = []color.Color{
		c(ActiveTheme.Border),
		c(ActiveTheme.Muted),
		c(ActiveTheme.Frozen),
		c(darkenHex(ActiveTheme.Secondary, 0.7)),
		c(ActiveTheme.Secondary),
		c(ActiveTheme.Primary),
		c(ActiveTheme.Accent),
		c(lightenHex(ActiveTheme.Accent, 0.5)),
	}
}

// RenderVisualization parses a JSON viz block and dispatches to the appropriate renderer.
func RenderVisualization(vizJSON string, width int, frame int) string {
	var v VizData
	if err := json.Unmarshal([]byte(vizJSON), &v); err != nil {
		return ""
	}

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
		titleColor := emberBreathe(frame)
		titleStyle := lipgloss.NewStyle().
			Foreground(titleColor).
			Bold(true)
		b.WriteString(titleStyle.Render(v.Title))
		b.WriteString("\n")

		titleLen := lipgloss.Width(v.Title)
		if titleLen > 0 {
			for i := range titleLen {
				gIdx := i * len(vizBorderGradient) / titleLen
				if gIdx >= len(vizBorderGradient) {
					gIdx = len(vizBorderGradient) - 1
				}
				style := lipgloss.NewStyle().Foreground(vizBorderGradient[gIdx])
				b.WriteString(style.Render("\u2500"))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString(body)
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func renderBarChart(v VizData, width int) string {
	var items []BarItem
	if err := json.Unmarshal(v.Data, &items); err != nil {
		return ""
	}
	if len(items) == 0 {
		return ""
	}

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

	valueWidth := 7
	barMaxWidth := width - maxLabel - valueWidth - 3
	if barMaxWidth < 10 {
		barMaxWidth = 10
	}

	labelStyle := lipgloss.NewStyle().Foreground(ColorText).Width(maxLabel).Align(lipgloss.Right)
	valueStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	var b strings.Builder
	for _, item := range items {
		barLen := int(math.Round((item.Value / maxVal) * float64(barMaxWidth)))
		if barLen < 1 && item.Value > 0 {
			barLen = 1
		}

		var bar strings.Builder
		for j := range barLen {
			gIdx := j * len(vizBarGradient) / barLen
			if gIdx >= len(vizBarGradient) {
				gIdx = len(vizBarGradient) - 1
			}
			style := lipgloss.NewStyle().Foreground(vizBarGradient[gIdx])
			bar.WriteString(style.Render("\u2588"))
		}

		label := labelStyle.Render(item.Label)
		val := valueStyle.Render(fmt.Sprintf(" %.0f", item.Value))

		b.WriteString(label + " " + bar.String() + val + "\n")
	}
	return b.String()
}

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
	altRowColor := lipgloss.Color(blendHex(ActiveTheme.Text, ActiveTheme.Muted, 0.4))
	evenStyle := cellStyle.Foreground(altRowColor)

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
		norm := (val - minVal) / valRange
		idx := int(math.Round(norm * 7))
		if idx > 7 {
			idx = 7
		}

		colorIdx := int(math.Round(norm * float64(len(sparkColors)-1)))
		if colorIdx >= len(sparkColors) {
			colorIdx = len(sparkColors) - 1
		}

		style := lipgloss.NewStyle().Foreground(sparkColors[colorIdx])
		b.WriteString(style.Render(string(sparkBlocks[idx])))
	}

	annotStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	b.WriteString(annotStyle.Render(fmt.Sprintf("  %.0f-%.0f", minVal, maxVal)))

	return b.String()
}

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

func renderProgress(v VizData, width int) string {
	maxVal := v.Max
	if maxVal == 0 {
		maxVal = 100
	}
	pct := v.vizValueFloat() / maxVal
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

	barWidth := width - len(label) - 10
	if barWidth < 10 {
		barWidth = 10
	}

	filled := int(math.Round(pct * float64(barWidth)))
	empty := barWidth - filled

	emptyStyle := lipgloss.NewStyle().Foreground(ColorBorder)
	labelStyle := lipgloss.NewStyle().Foreground(ColorText)
	pctStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	var barBuf strings.Builder
	for j := range filled {
		gIdx := j * len(vizBarGradient) / max(filled, 1)
		if gIdx >= len(vizBarGradient) {
			gIdx = len(vizBarGradient) - 1
		}
		style := lipgloss.NewStyle().Foreground(vizBarGradient[gIdx])
		barBuf.WriteString(style.Render("\u2588"))
	}
	bar := barBuf.String() + emptyStyle.Render(strings.Repeat("\u2591", empty))
	return labelStyle.Render(label) + " " + bar + " " + pctStyle.Render(fmt.Sprintf("%.0f%%", pct*100)) + "\n"
}

func renderGauge(v VizData, width int) string {
	maxVal := v.Max
	if maxVal == 0 {
		maxVal = 100
	}
	val := v.vizValueFloat()
	pct := val / maxVal
	if pct > 1 {
		pct = 1
	}

	unit := v.Unit
	label := v.Label
	if label == "" {
		label = "Value"
	}

	gaugeWidth := width/2 - 4
	if gaugeWidth < 10 {
		gaugeWidth = 10
	}

	filled := int(math.Round(pct * float64(gaugeWidth)))
	empty := gaugeWidth - filled

	gaugeGradientRamp := lipgloss.Blend1D(max(filled, 1),
		c(ActiveTheme.Muted),
		c(ActiveTheme.Frozen),
		c(ActiveTheme.Secondary),
		c(ActiveTheme.Primary),
		c(ActiveTheme.Error),
		c(darkenHex(ActiveTheme.Error, 0.7)),
	)

	emptyStyle := lipgloss.NewStyle().Foreground(ColorBorder)
	labelStyle := lipgloss.NewStyle().Foreground(ColorText).Bold(true)
	valueStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	var barBuf strings.Builder
	barBuf.WriteString("[")
	for j := range filled {
		gIdx := j
		if gIdx >= len(gaugeGradientRamp) {
			gIdx = len(gaugeGradientRamp) - 1
		}
		style := lipgloss.NewStyle().Foreground(gaugeGradientRamp[gIdx])
		barBuf.WriteString(style.Render("\u2588"))
	}
	barBuf.WriteString(emptyStyle.Render(strings.Repeat("\u2591", empty)))
	barBuf.WriteString("]")
	bar := barBuf.String()
	valStr := fmt.Sprintf("%.0f/%.0f", val, maxVal)
	if unit != "" {
		valStr += " " + unit
	}

	return labelStyle.Render(label) + "\n" + bar + " " + valueStyle.Render(valStr) + "\n"
}

func renderHeatmap(v VizData, _ int) string {
	var grid [][]float64
	if err := json.Unmarshal(v.Data, &grid); err != nil {
		return ""
	}
	if len(grid) == 0 {
		return ""
	}

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

	heatColors := []color.Color{
		c(darkenHex(ActiveTheme.Border, 0.7)),
		c(darkenHex(ActiveTheme.Border, 0.5)),
		c(ActiveTheme.Muted),
		c(ActiveTheme.Secondary),
		c(ActiveTheme.Primary),
		c(ActiveTheme.Accent),
		c(lightenHex(ActiveTheme.Accent, 0.5)),
	}

	maxLabelW := 2
	for i := range grid {
		label := fmt.Sprintf("%d", i)
		if i < len(v.Items) {
			label = v.Items[i]
		}
		if len(label) > maxLabelW {
			maxLabelW = len(label)
		}
	}

	var b strings.Builder
	if len(v.Headers) > 0 {
		headerStyle := lipgloss.NewStyle().Foreground(ColorMuted)
		b.WriteString(strings.Repeat(" ", maxLabelW+1))
		for _, h := range v.Headers {
			b.WriteString(headerStyle.Render(fmt.Sprintf("%-4s", h)))
		}
		b.WriteString("\n")
	}

	for i, row := range grid {
		rowLabel := fmt.Sprintf("%d", i)
		if i < len(v.Items) {
			rowLabel = v.Items[i]
		}
		labelStyle := lipgloss.NewStyle().Foreground(ColorMuted).Width(maxLabelW).Align(lipgloss.Right)
		b.WriteString(labelStyle.Render(rowLabel) + " ")

		for _, val := range row {
			norm := (val - minVal) / valRange
			colorIdx := int(math.Round(norm * float64(len(heatColors)-1)))
			if colorIdx >= len(heatColors) {
				colorIdx = len(heatColors) - 1
			}
			style := lipgloss.NewStyle().Foreground(heatColors[colorIdx])
			b.WriteString(style.Render("\u2588\u2588") + "  ")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderTimeline(v VizData) string {
	if len(v.Events) == 0 {
		return ""
	}

	timeStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	lineStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	dotStyle := lipgloss.NewStyle().Foreground(ColorSecondary)
	textStyle := lipgloss.NewStyle().Foreground(ColorText)

	var b strings.Builder
	for i, event := range v.Events {
		b.WriteString(timeStyle.Render(event.Time))
		b.WriteString(" " + dotStyle.Render("\u25CF") + " ")
		b.WriteString(textStyle.Render(event.Label))
		b.WriteString("\n")
		if i < len(v.Events)-1 {
			padding := strings.Repeat(" ", len(event.Time)+1)
			b.WriteString(padding + lineStyle.Render("\u2502") + "\n")
		}
	}
	return b.String()
}

func renderKV(v VizData) string {
	if len(v.Entries) == 0 {
		return ""
	}

	maxKey := 0
	for _, e := range v.Entries {
		if len(e.Key) > maxKey {
			maxKey = len(e.Key)
		}
	}

	keyStyle := lipgloss.NewStyle().Foreground(ColorSecondary).Bold(true).Width(maxKey).Align(lipgloss.Right)
	sepStyle := lipgloss.NewStyle().Foreground(ColorMuted)
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

func renderStat(v VizData) string {
	label := v.Label
	if label == "" {
		label = "Value"
	}
	unit := v.Unit

	valueStr := v.vizValueString()
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
		if strings.HasPrefix(v.Delta, "+") || strings.HasPrefix(v.Delta, "\u25B2") {
			deltaColor = c(ActiveTheme.Accent)
		} else if strings.HasPrefix(v.Delta, "-") || strings.HasPrefix(v.Delta, "\u25BC") {
			deltaColor = c(ActiveTheme.Secondary)
		} else {
			deltaColor = ColorMuted
		}
		deltaStyle := lipgloss.NewStyle().Foreground(deltaColor)
		b.WriteString(deltaStyle.Render(v.Delta) + "\n")
	}
	return b.String()
}

func renderDiff(v VizData) string {
	if len(v.OldLines) == 0 && len(v.NewLines) == 0 {
		return ""
	}

	removeStyle := lipgloss.NewStyle().Foreground(ColorSecondary)
	addStyle := lipgloss.NewStyle().Foreground(ColorAccent)
	contextStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	var b strings.Builder

	for _, line := range v.OldLines {
		b.WriteString(removeStyle.Render("- " + line) + "\n")
	}
	for _, line := range v.NewLines {
		b.WriteString(addStyle.Render("+ " + line) + "\n")
	}

	if len(v.OldLines) > 0 && len(v.NewLines) > 0 {
		summary := fmt.Sprintf("%d removed, %d added", len(v.OldLines), len(v.NewLines))
		b.WriteString(contextStyle.Render(summary) + "\n")
	}
	return b.String()
}

func renderList(v VizData) string {
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

	anyItems := make([]any, len(items))
	for i, item := range items {
		anyItems[i] = item
	}

	l := list.New(anyItems...).
		EnumeratorStyle(enumStyle).
		ItemStyle(itemStyle)

	return l.String()
}
