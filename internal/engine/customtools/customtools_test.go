package customtools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseToolManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool.yaml")

	content := `name: greeter
description: Greets a user by name
inputs:
  name:
    type: string
    description: The name to greet
    required: true
  loud:
    type: boolean
    description: Whether to shout
    required: false
command: "echo Hello {{name}}"
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	m, err := ParseToolManifest(path)
	require.NoError(t, err)

	assert.Equal(t, "greeter", m.Name)
	assert.Equal(t, "Greets a user by name", m.Description)
	assert.Len(t, m.Inputs, 2)
	assert.Equal(t, "string", m.Inputs["name"].Type)
	assert.True(t, m.Inputs["name"].Required)
	assert.False(t, m.Inputs["loud"].Required)
	assert.Equal(t, "echo Hello {{name}}", m.Command)
	assert.Empty(t, m.Stdin)
}

func TestCustomToolCallStdin(t *testing.T) {
	dir := t.TempDir()
	tool := &CustomTool{
		Manifest: ToolManifest{
			Name:    "json-echo",
			Command: "cat",
			Stdin:   "json",
		},
		Dir: dir,
	}

	input := json.RawMessage(`{"key":"value","num":42}`)
	result, err := tool.Call(context.Background(), input)
	require.NoError(t, err)
	assert.JSONEq(t, `{"key":"value","num":42}`, result)
}

func TestCustomToolCallTemplate(t *testing.T) {
	dir := t.TempDir()
	tool := &CustomTool{
		Manifest: ToolManifest{
			Name:    "greeter",
			Command: "echo Hello {{name}}",
		},
		Dir: dir,
	}

	input := json.RawMessage(`{"name":"providence"}`)
	result, err := tool.Call(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, "Hello providence\n", result)
}

func TestCustomToolTimeout(t *testing.T) {
	dir := t.TempDir()
	tool := &CustomTool{
		Manifest: ToolManifest{
			Name:    "sleeper",
			Command: "sleep 30",
		},
		Dir: dir,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := tool.Call(ctx, nil)
	require.Error(t, err)
	// The parent context times out before the tool's internal 120s timeout,
	// so the command gets killed either way.
	assert.Contains(t, err.Error(), "sleeper")
}

func TestLoadCustomToolsFromDir(t *testing.T) {
	projectDir := t.TempDir()
	homeDir := t.TempDir()

	// Create project tool.
	toolDir := filepath.Join(projectDir, ".providence", "tools", "lister")
	require.NoError(t, os.MkdirAll(toolDir, 0o755))
	manifest := `name: lister
description: Lists files
command: "ls"
`
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, "tool.yaml"), []byte(manifest), 0o644))

	// Create second project tool.
	toolDir2 := filepath.Join(projectDir, ".providence", "tools", "counter")
	require.NoError(t, os.MkdirAll(toolDir2, 0o755))
	manifest2 := `name: counter
description: Counts things
command: "wc -l"
stdin: json
`
	require.NoError(t, os.WriteFile(filepath.Join(toolDir2, "tool.yaml"), []byte(manifest2), 0o644))

	tools, err := LoadCustomTools(projectDir, homeDir)
	require.NoError(t, err)
	assert.Len(t, tools, 2)

	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Manifest.Name] = true
	}
	assert.True(t, names["lister"])
	assert.True(t, names["counter"])
}
