package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadMCPConfig_ProjectFile(t *testing.T) {
	dir := t.TempDir()
	data := `{
		"mcpServers": {
			"test-server": {
				"command": "npx",
				"args": ["-y", "@test/mcp-server"],
				"env": {"TOKEN": "abc"}
			}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(data), 0644))

	configs, err := LoadMCPConfig(dir, t.TempDir())
	require.NoError(t, err)
	require.Len(t, configs, 1)
	assert.Equal(t, "test-server", configs[0].Name)
	assert.Equal(t, "stdio", configs[0].Type)
	assert.Equal(t, "npx", configs[0].Command)
	assert.Equal(t, []string{"-y", "@test/mcp-server"}, configs[0].Args)
	assert.Equal(t, map[string]string{"TOKEN": "abc"}, configs[0].Env)
}

func TestLoadMCPConfig_UserFile(t *testing.T) {
	home := t.TempDir()
	provDir := filepath.Join(home, ".providence")
	require.NoError(t, os.MkdirAll(provDir, 0755))

	data := `{
		"mcpServers": {
			"global-server": {
				"type": "stdio",
				"command": "my-server",
				"args": ["--port", "3000"]
			}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(provDir, "mcp.json"), []byte(data), 0644))

	configs, err := LoadMCPConfig(t.TempDir(), home)
	require.NoError(t, err)
	require.Len(t, configs, 1)
	assert.Equal(t, "global-server", configs[0].Name)
	assert.Equal(t, "my-server", configs[0].Command)
}

func TestLoadMCPConfig_ProjectOverridesUser(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	provDir := filepath.Join(home, ".providence")
	require.NoError(t, os.MkdirAll(provDir, 0755))

	userConfig := `{"mcpServers": {"myserver": {"command": "user-cmd"}}}`
	projectConfig := `{"mcpServers": {"myserver": {"command": "project-cmd"}}}`

	require.NoError(t, os.WriteFile(filepath.Join(provDir, "mcp.json"), []byte(userConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(project, ".mcp.json"), []byte(projectConfig), 0644))

	configs, err := LoadMCPConfig(project, home)
	require.NoError(t, err)
	require.Len(t, configs, 1)
	assert.Equal(t, "project-cmd", configs[0].Command) // project wins
}

func TestLoadMCPConfig_NoFiles(t *testing.T) {
	configs, err := LoadMCPConfig(t.TempDir(), t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, configs)
}

func TestLoadMCPConfig_SkipsSSE(t *testing.T) {
	dir := t.TempDir()
	data := `{
		"mcpServers": {
			"sse-server": {
				"type": "sse",
				"url": "http://localhost:3000"
			},
			"stdio-server": {
				"command": "my-tool"
			}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(data), 0644))

	configs, err := LoadMCPConfig(dir, t.TempDir())
	require.NoError(t, err)
	require.Len(t, configs, 1)
	assert.Equal(t, "stdio-server", configs[0].Name)
}

func TestLoadMCPConfig_SkipsEmptyCommand(t *testing.T) {
	dir := t.TempDir()
	data := `{"mcpServers": {"empty": {"command": ""}}}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(data), 0644))

	configs, err := LoadMCPConfig(dir, t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, configs)
}
