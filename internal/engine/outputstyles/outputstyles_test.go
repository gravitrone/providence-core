package outputstyles

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStyleFile_WithFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concise.md")
	content := `---
name: concise
description: Short and direct
keep-coding-instructions: true
---

Be concise. No fluff.
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	style, err := ParseStyleFile(path)
	require.NoError(t, err)
	assert.Equal(t, "concise", style.Name)
	assert.Equal(t, "Short and direct", style.Description)
	assert.True(t, style.KeepCodingInstructions)
	assert.Equal(t, "Be concise. No fluff.", style.Prompt)
}

func TestParseStyleFile_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "raw.md")
	require.NoError(t, os.WriteFile(path, []byte("Just a prompt body."), 0o644))

	style, err := ParseStyleFile(path)
	require.NoError(t, err)
	assert.Equal(t, "", style.Name)
	assert.Equal(t, "Just a prompt body.", style.Prompt)
}

func TestLoadOutputStyles_ProjectOverridesUser(t *testing.T) {
	projectDir := t.TempDir()
	homeDir := t.TempDir()

	// Create project-level style.
	projStyleDir := filepath.Join(projectDir, ".providence", "output-styles")
	require.NoError(t, os.MkdirAll(projStyleDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projStyleDir, "brief.md"), []byte("---\nname: brief\n---\nProject brief."), 0o644))

	// Create user-level style with same name.
	userStyleDir := filepath.Join(homeDir, ".providence", "output-styles")
	require.NoError(t, os.MkdirAll(userStyleDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(userStyleDir, "brief.md"), []byte("---\nname: brief\n---\nUser brief."), 0o644))

	// Create user-level style with different name.
	require.NoError(t, os.WriteFile(filepath.Join(userStyleDir, "verbose.md"), []byte("---\nname: verbose\n---\nUser verbose."), 0o644))

	styles, err := LoadOutputStyles(projectDir, homeDir)
	require.NoError(t, err)

	assert.Len(t, styles, 2)

	var brief, verbose *OutputStyle
	for i := range styles {
		switch styles[i].Name {
		case "brief":
			brief = &styles[i]
		case "verbose":
			verbose = &styles[i]
		}
	}

	require.NotNil(t, brief)
	assert.Equal(t, "Project brief.", brief.Prompt, "project-level should override user-level")

	require.NotNil(t, verbose)
	assert.Equal(t, "User verbose.", verbose.Prompt)
}

func TestLoadOutputStyles_EmptyDirs(t *testing.T) {
	styles, err := LoadOutputStyles(t.TempDir(), t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, styles)
}

func TestLoadOutputStyles_NonexistentDirs(t *testing.T) {
	styles, err := LoadOutputStyles("/nonexistent/project", "/nonexistent/home")
	require.NoError(t, err)
	assert.Empty(t, styles)
}
