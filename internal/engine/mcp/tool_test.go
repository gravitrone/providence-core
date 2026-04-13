package mcp

import (
	"testing"

	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
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

// --- ParseToolName roundtrip ---

func TestBuildAndParseToolNameRoundtrip(t *testing.T) {
	tests := []struct {
		server string
		tool   string
	}{
		{"filesystem", "read_file"},
		{"my-server", "do_thing"},
		{"context7", "query-docs"},
		{"simple", "tool"},
	}
	for _, tc := range tests {
		t.Run(tc.server+"/"+tc.tool, func(t *testing.T) {
			qualified := BuildToolName(tc.server, tc.tool)
			gotServer, gotTool, ok := ParseToolName(qualified)
			assert.True(t, ok)
			assert.Equal(t, NormalizeName(tc.server), gotServer)
			assert.Equal(t, NormalizeName(tc.tool), gotTool)
		})
	}
}

func TestBuildAndParseToolNameSpecialCharsRoundtrip(t *testing.T) {
	qualified := BuildToolName("@scope/pkg", "do.thing")
	gotServer, gotTool, ok := ParseToolName(qualified)
	assert.True(t, ok)
	assert.Equal(t, "_scope_pkg", gotServer)
	assert.Equal(t, "do_thing", gotTool)
}

func TestParseToolNameEmptyString(t *testing.T) {
	_, _, ok := ParseToolName("")
	assert.False(t, ok)
}

func TestParseToolNameOnlyPrefix(t *testing.T) {
	_, _, ok := ParseToolName("mcp__")
	assert.False(t, ok)
}

// --- Execute delegates to manager ---

func TestMCPToolExecuteNoServer(t *testing.T) {
	mgr := NewManager()
	def := ToolDef{Name: "some_tool"}
	tool := NewMCPTool("nonexistent-server", def, mgr)

	result := tool.Execute(nil, map[string]any{"key": "val"})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "not connected")
}

func TestMCPToolExecuteErrorContent(t *testing.T) {
	// When the manager returns an error, the MCPTool should wrap it.
	mgr := NewManager()
	def := ToolDef{Name: "fail_tool"}
	tool := NewMCPTool("missing", def, mgr)

	result := tool.Execute(nil, nil)
	assert.True(t, result.IsError)
	assert.NotEmpty(t, result.Content)
}

// --- RegisterMCPTools ---

func TestRegisterMCPToolsEmpty(t *testing.T) {
	mgr := NewManager()
	registry := tools.NewRegistry()
	count := RegisterMCPTools(mgr, registry)
	assert.Equal(t, 0, count)
	assert.Empty(t, registry.All())
}

// --- Name format correctness ---

func TestBuildToolNameFormat(t *testing.T) {
	name := BuildToolName("server", "tool")
	assert.True(t, len(name) > 0)
	assert.Equal(t, "mcp__server__tool", name)

	// Verify double underscore separators.
	parts := splitToolName(name)
	assert.Equal(t, 3, len(parts))
	assert.Equal(t, "mcp", parts[0])
	assert.Equal(t, "server", parts[1])
	assert.Equal(t, "tool", parts[2])
}

func splitToolName(name string) []string {
	// Split on __ but only for the first two occurrences (prefix, server, tool).
	var parts []string
	rest := name
	for i := 0; i < 2; i++ {
		idx := indexOf(rest, "__")
		if idx < 0 {
			break
		}
		parts = append(parts, rest[:idx])
		rest = rest[idx+2:]
	}
	parts = append(parts, rest)
	return parts
}

func indexOf(s, sep string) int {
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}

// --- NormalizeName edge cases ---

func TestNormalizeNameEmpty(t *testing.T) {
	assert.Equal(t, "", NormalizeName(""))
}

func TestNormalizeNameAlreadyClean(t *testing.T) {
	assert.Equal(t, "clean_name-123", NormalizeName("clean_name-123"))
}

func TestNormalizeNameAllSpecial(t *testing.T) {
	assert.Equal(t, "___", NormalizeName("@./"))
}
