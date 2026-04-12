package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSkillFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-skill.md")

	content := `---
name: deploy
description: Deploy the application
when_to_use: When user asks to deploy
allowed-tools:
  - Bash
  - Read
model: opus
effort: high
---
You are a deployment assistant.

Run the deploy script and verify it succeeded.
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	skill, err := ParseSkillFile(path)
	require.NoError(t, err)

	assert.Equal(t, "deploy", skill.Name)
	assert.Equal(t, "Deploy the application", skill.Description)
	assert.Equal(t, "When user asks to deploy", skill.WhenToUse)
	assert.Equal(t, []string{"Bash", "Read"}, skill.AllowedTools)
	assert.Equal(t, "opus", skill.Model)
	assert.Equal(t, "high", skill.Effort)
	assert.Equal(t, path, skill.FilePath)
	assert.Contains(t, skill.Prompt, "You are a deployment assistant.")
	assert.Contains(t, skill.Prompt, "Run the deploy script")
}

func TestParseSkillFileNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "simple.md")

	content := `Just a plain markdown skill prompt.

Do the thing.
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	skill, err := ParseSkillFile(path)
	require.NoError(t, err)

	assert.Equal(t, "", skill.Name)
	assert.Equal(t, "", skill.Description)
	assert.Contains(t, skill.Prompt, "Just a plain markdown skill prompt.")
	assert.Contains(t, skill.Prompt, "Do the thing.")
}

func TestLoadSkillsFromDir(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, ".providence", "skills")
	require.NoError(t, os.MkdirAll(skillsDir, 0o755))

	skill1 := `---
name: alpha
description: First skill
---
Alpha prompt.
`
	skill2 := `---
name: beta
description: Second skill
---
Beta prompt.
`
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "alpha.md"), []byte(skill1), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "beta.md"), []byte(skill2), 0o644))

	// Also write a non-md file that should be ignored.
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "ignore.txt"), []byte("not a skill"), 0o644))

	skills, err := LoadSkills(dir, t.TempDir())
	require.NoError(t, err)
	assert.Len(t, skills, 2)

	names := map[string]bool{}
	for _, s := range skills {
		names[s.Name] = true
		assert.Equal(t, "project", s.Source)
	}
	assert.True(t, names["alpha"])
	assert.True(t, names["beta"])
}

func TestSkillDiscoveryOrder(t *testing.T) {
	projectDir := t.TempDir()
	homeDir := t.TempDir()

	// Project-level skill.
	projectSkills := filepath.Join(projectDir, ".providence", "skills")
	require.NoError(t, os.MkdirAll(projectSkills, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectSkills, "deploy.md"), []byte(`---
name: deploy
description: project deploy
---
Project deploy prompt.
`), 0o644))

	// User-level skill with same name - should be overridden.
	userSkills := filepath.Join(homeDir, ".providence", "skills")
	require.NoError(t, os.MkdirAll(userSkills, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(userSkills, "deploy.md"), []byte(`---
name: deploy
description: user deploy
---
User deploy prompt.
`), 0o644))

	// User-level unique skill - should be included.
	require.NoError(t, os.WriteFile(filepath.Join(userSkills, "monitor.md"), []byte(`---
name: monitor
description: user monitor
---
Monitor prompt.
`), 0o644))

	skills, err := LoadSkills(projectDir, homeDir)
	require.NoError(t, err)
	assert.Len(t, skills, 2)

	// The deploy skill should come from project, not user.
	for _, s := range skills {
		if s.Name == "deploy" {
			assert.Equal(t, "project", s.Source)
			assert.Equal(t, "project deploy", s.Description)
		}
		if s.Name == "monitor" {
			assert.Equal(t, "user", s.Source)
		}
	}
}
