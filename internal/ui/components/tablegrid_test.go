package components

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleColumns() []TableColumn {
	return []TableColumn{
		{Header: "Name", Width: 20, Align: lipgloss.Left},
		{Header: "Score", Width: 8, Align: lipgloss.Right},
		{Header: "Company", Width: 16, Align: lipgloss.Left},
	}
}

// --- TableGrid ---

func TestTableGridNotEmpty(t *testing.T) {
	cols := sampleColumns()
	rows := [][]string{{"Acme Corp SRE", "92", "Acme"}}
	out := TableGrid(cols, rows, 80)
	assert.NotEmpty(t, out)
}

func TestTableGridZeroWidthReturnsEmpty(t *testing.T) {
	cols := sampleColumns()
	rows := [][]string{{"x", "1", "y"}}
	out := TableGrid(cols, rows, 0)
	assert.Empty(t, out)
}

func TestTableGridNegativeWidthReturnsEmpty(t *testing.T) {
	out := TableGrid(sampleColumns(), [][]string{{"a", "b", "c"}}, -10)
	assert.Empty(t, out)
}

func TestTableGridNoColumnsReturnsPadded(t *testing.T) {
	out := TableGrid([]TableColumn{}, [][]string{{"a"}}, 40)
	// no columns - should return padded empty row
	assert.NotNil(t, out)
}

func TestTableGridContainsHeaders(t *testing.T) {
	cols := sampleColumns()
	out := TableGrid(cols, [][]string{}, 80)
	assert.Contains(t, out, "Name")
	assert.Contains(t, out, "Score")
	assert.Contains(t, out, "Company")
}

func TestTableGridContainsRowData(t *testing.T) {
	cols := sampleColumns()
	rows := [][]string{
		{"Backend Engineer", "88", "OpenAI"},
		{"ML Researcher", "95", "Anthropic"},
	}
	out := TableGrid(cols, rows, 100)
	assert.Contains(t, out, "OpenAI")
	assert.Contains(t, out, "Anthropic")
}

func TestTableGridMultipleRows(t *testing.T) {
	cols := sampleColumns()
	rows := make([][]string, 5)
	for i := range rows {
		rows[i] = []string{"Job Title", "80", "Company"}
	}
	out := TableGrid(cols, rows, 80)
	lines := strings.Split(out, "\n")
	// header + rule + 5 data rows = 7 lines minimum
	assert.GreaterOrEqual(t, len(lines), 7)
}

func TestTableGridEmptyRows(t *testing.T) {
	out := TableGrid(sampleColumns(), [][]string{}, 80)
	// should still render headers
	assert.Contains(t, out, "Name")
}

func TestTableGridTruncatesLongCellText(t *testing.T) {
	cols := []TableColumn{{Header: "Title", Width: 10, Align: lipgloss.Left}}
	rows := [][]string{{"This is a very long job title that exceeds width"}}
	out := TableGrid(cols, rows, 40)
	require.NotEmpty(t, out)
	// the rendered cell should be truncated with ellipsis
	assert.Contains(t, out, "...")
}

func TestTableGridWithActiveRow(t *testing.T) {
	cols := sampleColumns()
	rows := [][]string{
		{"ML Engineer", "91", "Google"},
		{"SRE", "72", "Meta"},
	}
	out := TableGridWithActiveRow(cols, rows, 100, 0)
	assert.Contains(t, out, "ML Engineer")
}

func TestTableGridActiveRowNegativeDisablesHighlight(t *testing.T) {
	cols := sampleColumns()
	rows := [][]string{{"Engineer", "80", "Company"}}
	// active row -1 means no highlight - should still render normally
	out := TableGridWithActiveRow(cols, rows, 80, -1)
	assert.Contains(t, out, "Engineer")
}

func TestSetTableGridActiveRowsEnabled(t *testing.T) {
	t.Cleanup(func() { SetTableGridActiveRowsEnabled(true) })

	SetTableGridActiveRowsEnabled(false)
	cols := sampleColumns()
	rows := [][]string{{"Engineer", "80", "Company"}}
	out := TableGridWithActiveRow(cols, rows, 80, 0)
	// should still render - just no highlight
	assert.Contains(t, out, "Engineer")
}

func TestTableGridHighlightSelectionMarkers(t *testing.T) {
	cols := []TableColumn{{Header: "Selected", Width: 12, Align: lipgloss.Left}}
	rows := [][]string{{"[X] applied"}}
	out := TableGrid(cols, rows, 40)
	// [X] gets highlighted via gridSelectedMarkStyle, but text still appears
	assert.Contains(t, out, "applied")
}

func TestTableGridSingleColumn(t *testing.T) {
	cols := []TableColumn{{Header: "Title", Width: 30, Align: lipgloss.Left}}
	rows := [][]string{{"Senior ML Engineer"}, {"Principal SRE"}}
	out := TableGrid(cols, rows, 50)
	assert.Contains(t, out, "Senior ML Engineer")
	assert.Contains(t, out, "Principal SRE")
}
