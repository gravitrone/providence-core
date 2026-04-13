package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with spaces", "with_spaces"},
		{"with.dots", "with_dots"},
		{"with/slashes", "with_slashes"},
		{"MiXeD-CaSe_ok", "MiXeD-CaSe_ok"},
		{"@scope/package", "_scope_package"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, NormalizeName(tc.input))
		})
	}
}

func TestBuildToolName(t *testing.T) {
	name := BuildToolName("my-server", "do_thing")
	assert.Equal(t, "mcp__my-server__do_thing", name)
}

func TestBuildToolNameNormalizesSpecialChars(t *testing.T) {
	name := BuildToolName("my server", "do.thing")
	assert.Equal(t, "mcp__my_server__do_thing", name)
}

func TestParseToolName(t *testing.T) {
	server, tool, ok := ParseToolName("mcp__my-server__do_thing")
	assert.True(t, ok)
	assert.Equal(t, "my-server", server)
	assert.Equal(t, "do_thing", tool)
}

func TestParseToolNameNotMCP(t *testing.T) {
	_, _, ok := ParseToolName("Bash")
	assert.False(t, ok)
}

func TestParseToolNameNoTool(t *testing.T) {
	_, _, ok := ParseToolName("mcp__server")
	assert.False(t, ok)
}

func TestParseToolNameWithDoubleUnderscoreInTool(t *testing.T) {
	server, tool, ok := ParseToolName("mcp__server__complex__tool")
	assert.True(t, ok)
	assert.Equal(t, "server", server)
	assert.Equal(t, "complex__tool", tool)
}

func TestMCPToolInterface(t *testing.T) {
	mgr := NewManager()
	def := ToolDef{
		Name:        "read-file",
		Description: "Read a file from disk",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
		},
	}

	tool := NewMCPTool("filesystem", def, mgr)

	assert.Equal(t, "mcp__filesystem__read-file", tool.Name())
	assert.Equal(t, "Read a file from disk", tool.Description())
	assert.False(t, tool.ReadOnly())

	schema := tool.InputSchema()
	assert.Equal(t, "object", schema["type"])
}

func TestMCPToolEmptyDescription(t *testing.T) {
	mgr := NewManager()
	def := ToolDef{Name: "some-tool"}
	tool := NewMCPTool("srv", def, mgr)
	assert.Contains(t, tool.Description(), "some-tool")
	assert.Contains(t, tool.Description(), "srv")
}

func TestMCPToolEmptySchema(t *testing.T) {
	mgr := NewManager()
	def := ToolDef{Name: "no-schema"}
	tool := NewMCPTool("srv", def, mgr)
	schema := tool.InputSchema()
	assert.Equal(t, "object", schema["type"])
}
