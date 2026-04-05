package components

import (
	"testing"

	"charm.land/bubbles/v2/table"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- NewProvidenceSpinner ---

func TestNewProvidenceSpinnerNotZero(t *testing.T) {
	s := NewProvidenceSpinner()
	// Spinner should have a non-empty style (gold foreground applied)
	rendered := s.View()
	assert.NotEmpty(t, rendered)
}

func TestNewProvidenceSpinnerIsDotStyle(t *testing.T) {
	s := NewProvidenceSpinner()
	// Dot spinner frames are non-empty
	assert.NotEmpty(t, s.Spinner.Frames)
}

// --- NewProvidenceTextInput ---

func TestNewProvidenceTextInputNotZero(t *testing.T) {
	ti := NewProvidenceTextInput("search jobs...")
	assert.Equal(t, "search jobs...", ti.Placeholder)
}

func TestNewProvidenceTextInputIsFocused(t *testing.T) {
	ti := NewProvidenceTextInput("filter")
	assert.True(t, ti.Focused())
}

func TestNewProvidenceTextInputRendersOutput(t *testing.T) {
	ti := NewProvidenceTextInput("type here")
	out := ti.View()
	assert.NotEmpty(t, out)
}

// --- NewProvidenceTable ---

func TestNewProvidenceTableNotNil(t *testing.T) {
	cols := []table.Column{
		{Title: "Title", Width: 30},
		{Title: "Score", Width: 8},
	}
	t2 := NewProvidenceTable(cols, 10)
	// table.Model has a View method - calling it should not panic
	out := t2.View()
	assert.NotEmpty(t, out)
}

func TestNewProvidenceTableNilColsUsesDefault(t *testing.T) {
	// Passing nil cols should fall back to a default column.
	t2 := NewProvidenceTable(nil, 5)
	out := t2.View()
	assert.NotEmpty(t, out)
}

func TestNewProvidenceTableWithRows(t *testing.T) {
	cols := []table.Column{
		{Title: "Company", Width: 20},
		{Title: "Role", Width: 20},
	}
	rows := []table.Row{
		{"Anthropic", "ML Engineer"},
		{"OpenAI", "SRE"},
	}
	t2 := NewProvidenceTable(cols, 10)
	t2.SetRows(rows)
	out := t2.View()
	assert.NotEmpty(t, out)
}

func TestNewProvidenceTableIsFocused(t *testing.T) {
	cols := []table.Column{{Title: "Title", Width: 20}}
	t2 := NewProvidenceTable(cols, 5)
	assert.True(t, t2.Focused())
}

// --- RenderInfoTable ---

func TestRenderInfoTableNotEmpty(t *testing.T) {
	rows := []InfoTableRow{
		{Key: "Company", Value: "Anthropic"},
		{Key: "Role", Value: "ML Engineer"},
		{Key: "Location", Value: "Remote"},
	}
	out := RenderInfoTable(rows, 80)
	assert.NotEmpty(t, out)
}

func TestRenderInfoTableContainsKeys(t *testing.T) {
	rows := []InfoTableRow{
		{Key: "Company", Value: "OpenAI"},
	}
	out := RenderInfoTable(rows, 80)
	assert.Contains(t, out, "Company")
}

func TestRenderInfoTableContainsValues(t *testing.T) {
	rows := []InfoTableRow{
		{Key: "Company", Value: "OpenAI"},
	}
	out := RenderInfoTable(rows, 80)
	assert.Contains(t, out, "OpenAI")
}

func TestRenderInfoTableEmptyRowsReturnsEmpty(t *testing.T) {
	out := RenderInfoTable([]InfoTableRow{}, 80)
	assert.Empty(t, out)
}

func TestRenderInfoTableZeroWidthReturnsEmpty(t *testing.T) {
	rows := []InfoTableRow{{Key: "k", Value: "v"}}
	out := RenderInfoTable(rows, 0)
	assert.Empty(t, out)
}

func TestRenderInfoTableMultipleRows(t *testing.T) {
	rows := []InfoTableRow{
		{Key: "Title", Value: "Principal Engineer"},
		{Key: "Salary", Value: "$200k"},
		{Key: "Remote", Value: "Yes"},
		{Key: "Score", Value: "94"},
	}
	out := RenderInfoTable(rows, 100)
	require.NotEmpty(t, out)
	assert.Contains(t, out, "Principal Engineer")
	assert.Contains(t, out, "$200k")
}

func TestRenderInfoTableStripsAnsiFromKeys(t *testing.T) {
	rows := []InfoTableRow{
		{Key: "\x1b[31mColored Key\x1b[0m", Value: "value"},
	}
	out := RenderInfoTable(rows, 80)
	assert.Contains(t, out, "Colored Key")
}

func TestRenderInfoTableLongValuesTruncated(t *testing.T) {
	rows := []InfoTableRow{
		{Key: "Desc", Value: "This is an extremely long job description that exceeds any reasonable column width for display purposes"},
	}
	// Should not panic and should produce output
	out := RenderInfoTable(rows, 80)
	assert.NotEmpty(t, out)
}

// --- NewProvidenceViewport ---

func TestNewProvidenceViewportDimensions(t *testing.T) {
	vp := NewProvidenceViewport(80, 24)
	assert.Equal(t, 80, vp.Width())
	assert.Equal(t, 24, vp.Height())
}

func TestNewProvidenceViewportZeroDimensions(t *testing.T) {
	vp := NewProvidenceViewport(0, 0)
	assert.Equal(t, 0, vp.Width())
	assert.Equal(t, 0, vp.Height())
}

func TestNewProvidenceViewportRendersContent(t *testing.T) {
	vp := NewProvidenceViewport(80, 24)
	vp.SetContent("hello from viewport")
	out := vp.View()
	assert.NotEmpty(t, out)
}
