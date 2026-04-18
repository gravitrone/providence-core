package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActivateForPathsMatchesGlob(t *testing.T) {
	projectDir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Chdir(projectDir)

	skillsDir := filepath.Join(projectDir, ".providence", "skills")
	require.NoError(t, os.MkdirAll(skillsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "go.md"), []byte(`---
name: go-rules
path_globs:
  - "internal/*.go"
---
Prefer the Go rules skill.
`), 0o644))

	path := filepath.Join(projectDir, "internal", "activate.go")
	activated := ActivateForPaths([]string{path})

	require.Len(t, activated, 1)
	assert.Equal(t, "go-rules", activated[0].Name)
	assert.Equal(t, "internal/*.go", activated[0].MatchedGlob)
	assert.Equal(t, "Prefer the Go rules skill.", activated[0].Instructions)
}

func TestActivateForPathsNoMatchReturnsEmpty(t *testing.T) {
	projectDir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Chdir(projectDir)

	skillsDir := filepath.Join(projectDir, ".providence", "skills")
	require.NoError(t, os.MkdirAll(skillsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "go.md"), []byte(`---
name: go-rules
path_globs:
  - "internal/*.go"
---
Prefer the Go rules skill.
`), 0o644))

	path := filepath.Join(projectDir, "docs", "activate.md")
	activated := ActivateForPaths([]string{path})

	assert.Empty(t, activated)
}
