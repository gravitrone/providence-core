package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gravitrone/providence-core/internal/engine/subagent"
)

func TestWriteAgentMemoryToolNameAndSchema(t *testing.T) {
	tool := NewWriteAgentMemoryTool("researcher", "/tmp/project")
	assert.Equal(t, "WriteAgentMemory", tool.Name())
	assert.False(t, tool.ReadOnly(), "must not be read-only so permission chain asks")

	schema := tool.InputSchema()
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	_, hasScope := props["scope"]
	_, hasContent := props["content"]
	_, hasOp := props["operation"]
	assert.True(t, hasScope)
	assert.True(t, hasContent)
	assert.True(t, hasOp)
}

func TestWriteAgentMemoryToolAppendsUserScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()

	tool := NewWriteAgentMemoryTool("reviewer", project)
	res := tool.Execute(context.Background(), map[string]any{
		"scope":   "user",
		"content": "prefer goroutines over raw threads",
	})
	require.False(t, res.IsError, "unexpected error: %s", res.Content)

	data, err := os.ReadFile(filepath.Join(home, ".providence", "agent-memory", "reviewer", "user.md"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "prefer goroutines over raw threads")
}

func TestWriteAgentMemoryToolReplacesProjectScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()

	tool := NewWriteAgentMemoryTool("reviewer", project)
	// Seed an append first.
	_ = tool.Execute(context.Background(), map[string]any{
		"scope":   "project",
		"content": "initial note",
	})
	res := tool.Execute(context.Background(), map[string]any{
		"scope":     "project",
		"content":   "replacement note",
		"operation": "replace",
	})
	require.False(t, res.IsError, "unexpected error: %s", res.Content)

	data, err := os.ReadFile(filepath.Join(project, ".providence", "agent-memory", "reviewer", "project.md"))
	require.NoError(t, err)
	assert.Equal(t, "replacement note", string(data))
}

func TestWriteAgentMemoryToolRejectsLocal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()

	tool := NewWriteAgentMemoryTool("reviewer", project)
	res := tool.Execute(context.Background(), map[string]any{
		"scope":   "local",
		"content": "should fail",
	})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "read-only")
}

func TestWriteAgentMemoryToolRejectsMissingFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	tool := NewWriteAgentMemoryTool("reviewer", project)

	res := tool.Execute(context.Background(), map[string]any{"content": "nope"})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "scope")

	res = tool.Execute(context.Background(), map[string]any{"scope": "user", "content": " "})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "content")
}

func TestWriteAgentMemoryToolProjectScopeRequiresRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	tool := NewWriteAgentMemoryTool("reviewer", "")
	res := tool.Execute(context.Background(), map[string]any{
		"scope":   "project",
		"content": "orphaned",
	})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "project root")
}

func TestNewSubagentToolRegistryOnlyExposesMemoryTool(t *testing.T) {
	reg := NewSubagentToolRegistry("researcher", t.TempDir())

	tool, ok := reg.Get("WriteAgentMemory")
	require.True(t, ok)
	assert.Equal(t, "WriteAgentMemory", tool.Name())

	// The registry must NOT expose write-oriented core tools by default.
	for _, forbidden := range []string{"Bash", "Write", "Edit", "Read"} {
		_, present := reg.Get(forbidden)
		assert.False(t, present, "subagent registry leaked %q", forbidden)
	}
}

func TestWriteAgentMemoryToolUsesSubagentPackage(t *testing.T) {
	// Sanity guard: if someone swaps the underlying implementation, this test
	// still ensures both entry points agree on the on-disk layout.
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()

	tool := NewWriteAgentMemoryTool("researcher", project)
	res := tool.Execute(context.Background(), map[string]any{
		"scope":   "user",
		"content": "via tool",
	})
	require.False(t, res.IsError)

	blocks := subagent.LoadAgentMemory("researcher", project)
	require.Len(t, blocks, 1)
	assert.Equal(t, subagent.MemoryScopeUser, blocks[0].Scope)
	assert.Contains(t, blocks[0].Content, "via tool")
	assert.True(t, strings.HasPrefix(blocks[0].Content, "## "), "append format must include timestamp header")
}
