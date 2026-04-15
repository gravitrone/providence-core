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

func TestLoadSkillsFromDirLayout(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, ".claude", "skills")
	require.NoError(t, os.MkdirAll(skillsDir, 0o755))

	// Directory layout: skillsDir/foo/SKILL.md
	fooDir := filepath.Join(skillsDir, "foo")
	require.NoError(t, os.MkdirAll(fooDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fooDir, "SKILL.md"), []byte(`---
name: foo
description: Foo skill
---
Foo prompt.
`), 0o644))

	// Directory layout without name in frontmatter - name derived from dir.
	barDir := filepath.Join(skillsDir, "bar")
	require.NoError(t, os.MkdirAll(barDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(barDir, "SKILL.md"), []byte(`---
description: Bar skill
---
Bar prompt.
`), 0o644))

	// Subdir without SKILL.md should be ignored.
	noSkillDir := filepath.Join(skillsDir, "no-skill")
	require.NoError(t, os.MkdirAll(noSkillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(noSkillDir, "README.md"), []byte("not a skill"), 0o644))

	skills, err := LoadSkills(dir, t.TempDir())
	require.NoError(t, err)
	assert.Len(t, skills, 2)

	names := map[string]string{}
	for _, s := range skills {
		names[s.Name] = s.Description
	}
	assert.Equal(t, "Foo skill", names["foo"])
	assert.Equal(t, "Bar skill", names["bar"])
}

func TestLoadSkillsMixedLayouts(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, ".providence", "skills")
	require.NoError(t, os.MkdirAll(skillsDir, 0o755))

	// Flat layout.
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "flat.md"), []byte(`---
name: flat
description: Flat skill
---
Flat prompt.
`), 0o644))

	// Directory layout.
	dirSkill := filepath.Join(skillsDir, "dir-skill")
	require.NoError(t, os.MkdirAll(dirSkill, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dirSkill, "SKILL.md"), []byte(`---
name: dir-skill
description: Dir skill
---
Dir prompt.
`), 0o644))

	skills, err := LoadSkills(dir, t.TempDir())
	require.NoError(t, err)
	assert.Len(t, skills, 2)

	names := map[string]bool{}
	for _, s := range skills {
		names[s.Name] = true
	}
	assert.True(t, names["flat"])
	assert.True(t, names["dir-skill"])
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

// TestParseSkillFileMalformedYAMLReturnsError pins the error path for
// broken frontmatter. Silently dropping a malformed skill would make it
// invisible at runtime and confuse the user ("why does /skill foo say
// not found?"). ParseSkillFile must surface the YAML error so the
// caller can report it.
func TestParseSkillFileMalformedYAMLReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "broken.md")
	// Unclosed bracket in YAML block triggers a scanner error.
	require.NoError(t, os.WriteFile(path, []byte(`---
name: broken
description: [unclosed
---
body.
`), 0o644))

	_, err := ParseSkillFile(path)
	require.Error(t, err, "malformed YAML frontmatter must surface as an error")
	assert.Contains(t, err.Error(), "frontmatter", "error must name the culprit section")
}

// TestParseSkillFileMultilineFrontmatterFields verifies that YAML block
// scalar fields (description, when_to_use) survive intact through the
// frontmatter parser. A naive line-oriented parse would truncate at the
// first newline and silently drop context.
func TestParseSkillFileMultilineFrontmatterFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "multi.md")
	require.NoError(t, os.WriteFile(path, []byte(`---
name: multi
description: |
  line one of description
  line two of description
when_to_use: |
  first trigger
  second trigger
---
Prompt body here.
`), 0o644))

	sd, err := ParseSkillFile(path)
	require.NoError(t, err)
	require.NotNil(t, sd)

	assert.Equal(t, "multi", sd.Name)
	assert.Contains(t, sd.Description, "line one of description")
	assert.Contains(t, sd.Description, "line two of description")
	assert.Contains(t, sd.WhenToUse, "first trigger")
	assert.Contains(t, sd.WhenToUse, "second trigger")
	assert.Equal(t, "Prompt body here.", sd.Prompt)
}

// TestParseSkillFileNoFrontmatterTreatsWholeFileAsPrompt pins the
// frontmatter-less path: a plain .md with no YAML header must load as
// a skill whose Prompt is the whole body and whose fields are defaults.
func TestParseSkillFileNoFrontmatterTreatsWholeFileAsPrompt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "plain.md")
	require.NoError(t, os.WriteFile(path, []byte("just a prompt with no frontmatter at all."), 0o644))

	sd, err := ParseSkillFile(path)
	require.NoError(t, err)
	assert.Equal(t, "just a prompt with no frontmatter at all.", sd.Prompt)
	assert.Empty(t, sd.Name, "no frontmatter means no name in the struct - caller derives from filename")
	assert.Empty(t, sd.Description)
}
