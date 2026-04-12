package components

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeDiffLinesNoChanges(t *testing.T) {
	content := "line1\nline2\nline3"
	lines := ComputeDiffLines(content, content)
	for _, l := range lines {
		assert.Equal(t, DiffContext, l.Type, "identical content should only produce context lines")
	}
}

func TestComputeDiffLinesAdditions(t *testing.T) {
	old := "line1\nline3"
	new := "line1\nline2\nline3"
	lines := ComputeDiffLines(old, new)

	var adds int
	for _, l := range lines {
		if l.Type == DiffAdd {
			adds++
			assert.Equal(t, "line2", l.Content)
		}
	}
	assert.Equal(t, 1, adds, "should have exactly one addition")
}

func TestComputeDiffLinesDeletions(t *testing.T) {
	old := "line1\nline2\nline3"
	new := "line1\nline3"
	lines := ComputeDiffLines(old, new)

	var dels int
	for _, l := range lines {
		if l.Type == DiffDel {
			dels++
			assert.Equal(t, "line2", l.Content)
		}
	}
	assert.Equal(t, 1, dels, "should have exactly one deletion")
}

func TestRenderDiffAdditions(t *testing.T) {
	old := "func main() {\n}"
	new := "func main() {\n\tfmt.Println(\"hello\")\n}"
	theme := DefaultDiffTheme()

	output := RenderDiff(old, new, "main.go", 80, theme)
	require.NotEmpty(t, output)
	assert.Contains(t, output, "+")
	assert.Contains(t, output, "main.go")
}

func TestRenderDiffDeletions(t *testing.T) {
	old := "func main() {\n\tfmt.Println(\"hello\")\n}"
	new := "func main() {\n}"
	theme := DefaultDiffTheme()

	output := RenderDiff(old, new, "main.go", 80, theme)
	require.NotEmpty(t, output)
	assert.Contains(t, output, "-")
}

func TestRenderDiffWordLevel(t *testing.T) {
	// When lines are paired (del then add) and differ by less than 40%,
	// word-level highlighting should be used. For now we verify both + and - appear.
	old := "The quick brown fox jumps over the lazy dog"
	new := "The quick brown cat jumps over the lazy dog"
	theme := DefaultDiffTheme()

	output := RenderDiff(old, new, "", 80, theme)
	assert.Contains(t, output, "-")
	assert.Contains(t, output, "+")
}

func TestRenderDiffCollapsible(t *testing.T) {
	// Build a diff with >10 lines to trigger collapse.
	var oldLines, newLines []string
	for i := 0; i < 20; i++ {
		oldLines = append(oldLines, "old line")
	}
	for i := 0; i < 20; i++ {
		newLines = append(newLines, "new line")
	}
	old := strings.Join(oldLines, "\n")
	new := strings.Join(newLines, "\n")
	theme := DefaultDiffTheme()

	output := RenderDiff(old, new, "big.go", 80, theme)
	assert.Contains(t, output, "show")
	assert.Contains(t, output, "more lines")
}

func TestRenderDiffExpandedNoCollapse(t *testing.T) {
	var oldLines, newLines []string
	for i := 0; i < 20; i++ {
		oldLines = append(oldLines, "old line")
	}
	for i := 0; i < 20; i++ {
		newLines = append(newLines, "new line")
	}
	old := strings.Join(oldLines, "\n")
	new := strings.Join(newLines, "\n")
	theme := DefaultDiffTheme()

	output := RenderDiffExpanded(old, new, "big.go", 80, theme)
	assert.NotContains(t, output, "more lines", "expanded mode should not collapse")
}

func TestRenderDiffEmpty(t *testing.T) {
	theme := DefaultDiffTheme()
	output := RenderDiff("", "", "", 80, theme)
	assert.Empty(t, output, "no diff for identical empty content")
}

func TestRenderDiffZeroWidth(t *testing.T) {
	theme := DefaultDiffTheme()
	output := RenderDiff("a", "b", "", 0, theme)
	assert.NotEmpty(t, output, "zero width should use fallback")
}

func TestLerpHexSimple(t *testing.T) {
	// Black to white at 50%.
	result := lerpHexSimple("#000000", "#FFFFFF", 0.5)
	// Should be roughly gray.
	assert.True(t, strings.HasPrefix(result, "#"), "should be hex color")
	assert.Len(t, result, 7)

	// Endpoints.
	assert.Equal(t, "#000000", lerpHexSimple("#000000", "#FFFFFF", 0.0))
	assert.Equal(t, "#FFFFFF", lerpHexSimple("#000000", "#FFFFFF", 1.0))
}

func TestParseHex(t *testing.T) {
	r, g, b, ok := parseHex("#FF8800")
	assert.True(t, ok)
	assert.Equal(t, 0xFF, r)
	assert.Equal(t, 0x88, g)
	assert.Equal(t, 0x00, b)

	_, _, _, ok = parseHex("invalid")
	assert.False(t, ok)
}
