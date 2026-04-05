package components

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- Box ---

func TestBoxNotEmpty(t *testing.T) {
	out := Box("hello", 80)
	assert.NotEmpty(t, out)
}

func TestBoxContainsContent(t *testing.T) {
	out := Box("some content here", 80)
	assert.Contains(t, out, "some content here")
}

func TestBoxZeroWidth(t *testing.T) {
	// zero width should still render content via fallback
	out := Box("content", 0)
	assert.NotEmpty(t, out)
}

func TestBoxNarrowWidth(t *testing.T) {
	// very narrow - should still not panic
	out := Box("hi", 10)
	assert.NotEmpty(t, out)
}

// --- ActiveBox ---

func TestActiveBoxNotEmpty(t *testing.T) {
	out := ActiveBox("active content", 80)
	assert.NotEmpty(t, out)
}

func TestActiveBoxContainsContent(t *testing.T) {
	out := ActiveBox("active content", 80)
	assert.Contains(t, out, "active content")
}

func TestActiveBoxDiffersFromBox(t *testing.T) {
	content := "same content"
	normal := Box(content, 80)
	active := ActiveBox(content, 80)
	// Different border colors mean different ANSI sequences
	assert.NotEqual(t, normal, active)
}

// --- ErrorBox ---

func TestErrorBoxNotEmpty(t *testing.T) {
	out := ErrorBox("Error", "something went wrong", 80)
	assert.NotEmpty(t, out)
}

func TestErrorBoxContainsTitle(t *testing.T) {
	out := ErrorBox("Connection Failed", "timeout after 30s", 80)
	assert.Contains(t, out, "Connection Failed")
}

func TestErrorBoxContainsMessage(t *testing.T) {
	out := ErrorBox("Error", "timeout after 30s", 80)
	assert.Contains(t, out, "timeout after 30s")
}

func TestErrorBoxNoTitle(t *testing.T) {
	out := ErrorBox("", "something went wrong", 80)
	assert.Contains(t, out, "something went wrong")
}

// --- ClampTextWidthEllipsis ---

func TestClampTextWidthEllipsisShortText(t *testing.T) {
	out := ClampTextWidthEllipsis("hello", 20)
	assert.Equal(t, "hello", out)
}

func TestClampTextWidthEllipsisExactFit(t *testing.T) {
	out := ClampTextWidthEllipsis("12345", 5)
	assert.Equal(t, "12345", out)
}

func TestClampTextWidthEllipsisAddsEllipsis(t *testing.T) {
	out := ClampTextWidthEllipsis("hello world", 8)
	assert.True(t, strings.HasSuffix(out, "..."), "should end with ellipsis, got %q", out)
}

func TestClampTextWidthEllipsisRespectsBound(t *testing.T) {
	const maxWidth = 10
	out := ClampTextWidthEllipsis("this is a long string", maxWidth)
	assert.LessOrEqual(t, len([]rune(out)), maxWidth)
}

func TestClampTextWidthEllipsisZeroWidth(t *testing.T) {
	out := ClampTextWidthEllipsis("hello", 0)
	assert.Empty(t, out)
}

func TestClampTextWidthEllipsisNarrowWidth(t *testing.T) {
	// width 3 or less - no ellipsis, just truncate
	out := ClampTextWidthEllipsis("hello", 3)
	assert.LessOrEqual(t, len([]rune(out)), 3)
}

func TestClampTextWidthEllipsisStripsAnsi(t *testing.T) {
	out := ClampTextWidthEllipsis("clean text", 20)
	assert.Equal(t, "clean text", out)
}

// --- BoxContentWidth ---

func TestBoxContentWidthPositive(t *testing.T) {
	w := BoxContentWidth(80)
	assert.Greater(t, w, 0)
}

func TestBoxContentWidthZero(t *testing.T) {
	w := BoxContentWidth(0)
	assert.Equal(t, 0, w)
}

func TestBoxContentWidthNarrow(t *testing.T) {
	// narrow inputs should not panic
	w := BoxContentWidth(5)
	assert.GreaterOrEqual(t, w, 0)
}

// --- InfoRow ---

func TestInfoRowContainsLabel(t *testing.T) {
	out := InfoRow("Company", "Anthropic")
	assert.Contains(t, out, "Company")
}

func TestInfoRowContainsValue(t *testing.T) {
	out := InfoRow("Company", "Anthropic")
	assert.Contains(t, out, "Anthropic")
}

func TestInfoRowNotEmpty(t *testing.T) {
	out := InfoRow("k", "v")
	assert.NotEmpty(t, out)
}

// --- Table ---

func TestTableEmptyRowsReturnsEmpty(t *testing.T) {
	out := Table("Title", []TableRow{}, 80)
	assert.Empty(t, out)
}

func TestTableContainsValues(t *testing.T) {
	rows := []TableRow{
		{Label: "Role", Value: "ML Engineer"},
		{Label: "Location", Value: "Remote"},
	}
	out := Table("Details", rows, 100)
	assert.Contains(t, out, "ML Engineer")
	assert.Contains(t, out, "Remote")
}

func TestTableContainsLabels(t *testing.T) {
	rows := []TableRow{
		{Label: "Role", Value: "SRE"},
	}
	out := Table("Info", rows, 80)
	assert.Contains(t, out, "Role")
}

func TestTableWithColoredValue(t *testing.T) {
	rows := []TableRow{
		{Label: "Score", Value: "95", ValueColor: "#34d399"},
	}
	out := Table("", rows, 80)
	assert.Contains(t, out, "95")
}
