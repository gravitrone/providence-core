package subagent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAgentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-agent.md")

	content := `---
name: my-agent
description: A test agent
tools:
  - Read
  - Grep
model: fast
maxTurns: 10
---
You are a test agent. Do test things.`

	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	agent, err := ParseAgentFile(path)
	require.NoError(t, err)
	assert.Equal(t, "my-agent", agent.Name)
	assert.Equal(t, "A test agent", agent.Description)
	assert.Equal(t, []string{"Read", "Grep"}, agent.Tools)
	assert.Equal(t, "fast", agent.Model)
	assert.Equal(t, 10, agent.MaxTurns)
	assert.Equal(t, "You are a test agent. Do test things.", agent.SystemPrompt)
}

func TestParseAgentFileNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "simple.md")

	content := "Just a system prompt with no frontmatter."
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	agent, err := ParseAgentFile(path)
	require.NoError(t, err)
	assert.Equal(t, "", agent.Name)
	assert.Equal(t, "Just a system prompt with no frontmatter.", agent.SystemPrompt)
}

func TestLoadCustomAgentsFromDir(t *testing.T) {
	projectRoot := t.TempDir()
	homeDir := t.TempDir()

	agentDir := filepath.Join(projectRoot, ".providence", "agents")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))

	agent1 := `---
name: researcher
description: Research agent
tools:
  - Read
  - Grep
model: fast
---
You research things.`

	agent2 := `---
name: writer
description: Writing agent
tools:
  - Write
  - Edit
model: inherit
---
You write things.`

	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "researcher.md"), []byte(agent1), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "writer.md"), []byte(agent2), 0o644))

	agents, err := LoadCustomAgents(projectRoot, homeDir)
	require.NoError(t, err)
	assert.Len(t, agents, 2)
	assert.Equal(t, "Research agent", agents["researcher"].Description)
	assert.Equal(t, "Writing agent", agents["writer"].Description)
}

func TestLoaderValidFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "full-agent.md")

	content := `---
name: full-agent
description: Fully specified agent
tools:
  - Read
  - Write
  - Bash
disallowedTools:
  - Grep
model: fast
engine: claude
effort: high
maxTurns: 25
permissionMode: plan
background: true
isolation: docker
---
You are a fully specified agent.`

	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	agent, err := ParseAgentFile(path)
	require.NoError(t, err)
	assert.Equal(t, "full-agent", agent.Name)
	assert.Equal(t, "Fully specified agent", agent.Description)
	assert.Equal(t, []string{"Read", "Write", "Bash"}, agent.Tools)
	assert.Equal(t, []string{"Grep"}, agent.DisallowedTools)
	assert.Equal(t, "fast", agent.Model)
	assert.Equal(t, "claude", agent.Engine)
	assert.Equal(t, "high", agent.Effort)
	assert.Equal(t, 25, agent.MaxTurns)
	assert.Equal(t, "plan", agent.PermissionMode)
	assert.True(t, agent.Background)
	assert.Equal(t, "docker", agent.Isolation)
}

func TestLoaderBodyAsPrompt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt-agent.md")

	content := `---
name: prompter
---
This is the system prompt.
It spans multiple lines.
Third line here.`

	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	agent, err := ParseAgentFile(path)
	require.NoError(t, err)
	assert.Equal(t, "prompter", agent.Name)
	assert.Contains(t, agent.SystemPrompt, "This is the system prompt.")
	assert.Contains(t, agent.SystemPrompt, "Third line here.")
}

func TestLoaderProjectOverridesUser(t *testing.T) {
	projectRoot := t.TempDir()
	homeDir := t.TempDir()

	// Create same-named agent in both project and user dirs.
	projectDir := filepath.Join(projectRoot, ".providence", "agents")
	userDir := filepath.Join(homeDir, ".providence", "agents")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	require.NoError(t, os.MkdirAll(userDir, 0o755))

	projectAgent := `---
name: scout
description: Project scout
model: fast
---
Project version.`

	userAgent := `---
name: scout
description: User scout
model: slow
---
User version.`

	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "scout.md"), []byte(projectAgent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(userDir, "scout.md"), []byte(userAgent), 0o644))

	agents, err := LoadCustomAgents(projectRoot, homeDir)
	require.NoError(t, err)

	scout, ok := agents["scout"]
	require.True(t, ok)
	assert.Equal(t, "Project scout", scout.Description, "project-level should win over user-level")
	assert.Equal(t, "fast", scout.Model)
}
