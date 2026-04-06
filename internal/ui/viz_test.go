package ui

import (
	"strings"
	"testing"

	"github.com/gravitrone/providence-core/internal/ui/components"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clean strips ANSI escape codes for content assertions.
func clean(s string) string {
	return components.SanitizeText(s)
}

func TestRenderVisualization_Bar(t *testing.T) {
	vizJSON := `{"type": "bar", "title": "Coverage", "data": [{"label": "ui", "value": 85}, {"label": "engine", "value": 92}]}`
	result := RenderVisualization(vizJSON, 80)
	require.NotEmpty(t, result)
	plain := clean(result)
	assert.Contains(t, plain, "Coverage")
	assert.Contains(t, plain, "ui")
	assert.Contains(t, plain, "engine")
	assert.Contains(t, plain, "█")
	assert.Contains(t, plain, "85")
	assert.Contains(t, plain, "92")
}

func TestRenderVisualization_Table(t *testing.T) {
	vizJSON := `{"type": "table", "title": "Deps", "headers": ["Package", "Version"], "rows": [["bubbletea", "v2.0.2"], ["lipgloss", "v2.0.2"]]}`
	result := RenderVisualization(vizJSON, 60)
	require.NotEmpty(t, result)
	plain := clean(result)
	assert.Contains(t, plain, "Deps")
	assert.Contains(t, plain, "Package")
	assert.Contains(t, plain, "bubbletea")
	assert.Contains(t, plain, "lipgloss")
}

func TestRenderVisualization_Sparkline(t *testing.T) {
	vizJSON := `{"type": "sparkline", "title": "CPU", "data": [45, 62, 78, 55, 90, 82, 71]}`
	result := RenderVisualization(vizJSON, 80)
	require.NotEmpty(t, result)
	plain := clean(result)
	assert.Contains(t, plain, "CPU")
	// Should contain block chars.
	hasBlock := false
	for _, r := range plain {
		if r >= '▁' && r <= '█' {
			hasBlock = true
			break
		}
	}
	assert.True(t, hasBlock, "sparkline should contain block characters")
	// Should contain min-max annotation.
	assert.Contains(t, plain, "45-90")
}

func TestRenderVisualization_Tree(t *testing.T) {
	vizJSON := `{"type": "tree", "title": "Project", "root": {"name": "providence-core", "children": [{"name": "cmd/"}, {"name": "internal/", "children": [{"name": "engine/"}, {"name": "ui/"}]}]}}`
	result := RenderVisualization(vizJSON, 80)
	require.NotEmpty(t, result)
	plain := clean(result)
	assert.Contains(t, plain, "Project")
	assert.Contains(t, plain, "providence-core")
	assert.Contains(t, plain, "cmd/")
	assert.Contains(t, plain, "internal/")
	assert.Contains(t, plain, "engine/")
	assert.Contains(t, plain, "ui/")
}

func TestRenderVisualization_List(t *testing.T) {
	vizJSON := `{"type": "list", "title": "Tasks", "items": ["Build viz", "Write tests", "Update prompt"]}`
	result := RenderVisualization(vizJSON, 80)
	require.NotEmpty(t, result)
	plain := clean(result)
	assert.Contains(t, plain, "Tasks")
	assert.Contains(t, plain, "Build viz")
	assert.Contains(t, plain, "Write tests")
	assert.Contains(t, plain, "Update prompt")
}

func TestRenderVisualization_ListFromData(t *testing.T) {
	vizJSON := `{"type": "list", "title": "Items", "data": ["Alpha", "Beta", "Gamma"]}`
	result := RenderVisualization(vizJSON, 80)
	require.NotEmpty(t, result)
	plain := clean(result)
	assert.Contains(t, plain, "Alpha")
	assert.Contains(t, plain, "Beta")
}

func TestRenderVisualization_InvalidJSON(t *testing.T) {
	result := RenderVisualization("not json", 80)
	assert.Empty(t, result)
}

func TestRenderVisualization_UnknownType(t *testing.T) {
	result := RenderVisualization(`{"type": "pie", "title": "test"}`, 80)
	assert.Empty(t, result)
}

func TestRenderVisualization_EmptyBar(t *testing.T) {
	result := RenderVisualization(`{"type": "bar", "data": []}`, 80)
	assert.Empty(t, result)
}

func TestRenderVisualization_ZeroWidth(t *testing.T) {
	vizJSON := `{"type": "bar", "title": "Test", "data": [{"label": "a", "value": 50}]}`
	result := RenderVisualization(vizJSON, 0)
	require.NotEmpty(t, result, "should default to 80 width")
}

func TestProcessVizBlocks(t *testing.T) {
	content := "Here is a chart:\n\n```providence-viz\n{\"type\": \"bar\", \"title\": \"Test\", \"data\": [{\"label\": \"x\", \"value\": 100}]}\n```\n\nAnd some more text."
	result := ProcessVizBlocks(content, 80)
	plain := clean(result)
	// The viz block should be replaced with rendered output.
	assert.NotContains(t, result, "```providence-viz")
	assert.Contains(t, plain, "Test")
	assert.Contains(t, plain, "█")
	assert.Contains(t, plain, "And some more text.")
}

func TestProcessVizBlocks_MultipleBlocks(t *testing.T) {
	content := "```providence-viz\n{\"type\": \"bar\", \"title\": \"A\", \"data\": [{\"label\": \"x\", \"value\": 50}]}\n```\n\nMiddle text\n\n```providence-viz\n{\"type\": \"list\", \"title\": \"B\", \"items\": [\"one\", \"two\"]}\n```"
	result := ProcessVizBlocks(content, 80)
	plain := clean(result)
	assert.Contains(t, plain, "A")
	assert.Contains(t, plain, "B")
	assert.Contains(t, plain, "Middle text")
	assert.False(t, strings.Contains(result, "```providence-viz"))
}

func TestProcessVizBlocks_NoBlocks(t *testing.T) {
	content := "Just regular markdown with no viz blocks."
	result := ProcessVizBlocks(content, 80)
	assert.Equal(t, content, result)
}

func TestProcessVizBlocks_InvalidBlock(t *testing.T) {
	content := "```providence-viz\nnot valid json\n```"
	result := ProcessVizBlocks(content, 80)
	// Invalid blocks are left as-is.
	assert.Contains(t, result, "```providence-viz")
}

func TestRenderVisualization_SparklineFlat(t *testing.T) {
	// All same values - should still render.
	vizJSON := `{"type": "sparkline", "data": [50, 50, 50]}`
	result := RenderVisualization(vizJSON, 80)
	require.NotEmpty(t, result)
}

func TestRenderVisualization_TreeNoChildren(t *testing.T) {
	vizJSON := `{"type": "tree", "root": {"name": "leaf"}}`
	result := RenderVisualization(vizJSON, 80)
	require.NotEmpty(t, result)
	assert.Contains(t, clean(result), "leaf")
}

func TestRenderVisualization_NilRoot(t *testing.T) {
	vizJSON := `{"type": "tree", "title": "Empty"}`
	result := RenderVisualization(vizJSON, 80)
	assert.Empty(t, result)
}
