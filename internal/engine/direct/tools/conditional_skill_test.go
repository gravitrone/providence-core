package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gravitrone/providence-core/internal/engine/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupConditionalSkillProject(t *testing.T) string {
	t.Helper()

	projectDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Chdir(projectDir)

	skillsDir := filepath.Join(projectDir, ".providence", "skills")
	require.NoError(t, os.MkdirAll(skillsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "go.md"), []byte(`---
name: go-rules
path_globs:
  - "internal/*.go"
---
Follow the Go rules skill.
`), 0o644))

	return projectDir
}

func TestEditToolActivatesMatchingSkill(t *testing.T) {
	projectDir := setupConditionalSkillProject(t)

	path := filepath.Join(projectDir, "internal", "edit.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("before"), 0o644))

	fs := NewFileState()
	fs.MarkRead(path)

	editTool := NewEditTool(fs)
	var activated []skills.ActivatedSkill
	editTool.SetSkillActivationHandler(func(items []skills.ActivatedSkill) {
		activated = append(activated, items...)
	})

	result := editTool.Execute(context.Background(), map[string]any{
		"file_path":  path,
		"old_string": "before",
		"new_string": "after",
	})

	require.False(t, result.IsError, result.Content)
	require.NotNil(t, result.ContextModifier)

	result.ContextModifier()

	require.Len(t, activated, 1)
	assert.Equal(t, "go-rules", activated[0].Name)
	assert.Equal(t, "internal/*.go", activated[0].MatchedGlob)
	assert.Equal(t, "Follow the Go rules skill.", activated[0].Instructions)
}

func TestReadToolActivatesMatchingSkill(t *testing.T) {
	projectDir := setupConditionalSkillProject(t)

	path := filepath.Join(projectDir, "internal", "read.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("package tools\n"), 0o644))

	readTool, _ := newReadTool()
	var activated []skills.ActivatedSkill
	readTool.SetSkillActivationHandler(func(items []skills.ActivatedSkill) {
		activated = append(activated, items...)
	})

	result := readTool.Execute(context.Background(), map[string]any{"file_path": path})

	require.False(t, result.IsError, result.Content)
	require.NotNil(t, result.ContextModifier)

	result.ContextModifier()

	require.Len(t, activated, 1)
	assert.Equal(t, "go-rules", activated[0].Name)
	assert.Equal(t, "internal/*.go", activated[0].MatchedGlob)
	assert.Equal(t, "Follow the Go rules skill.", activated[0].Instructions)
}

func TestWriteToolActivatesMatchingSkill(t *testing.T) {
	projectDir := setupConditionalSkillProject(t)

	path := filepath.Join(projectDir, "internal", "write.go")
	writeTool := NewWriteTool(NewFileState())

	var activated []skills.ActivatedSkill
	writeTool.SetSkillActivationHandler(func(items []skills.ActivatedSkill) {
		activated = append(activated, items...)
	})

	result := writeTool.Execute(context.Background(), map[string]any{
		"file_path": path,
		"content":   "package tools\n",
	})

	require.False(t, result.IsError, result.Content)
	require.NotNil(t, result.ContextModifier)

	result.ContextModifier()

	require.Len(t, activated, 1)
	assert.Equal(t, "go-rules", activated[0].Name)
	assert.Equal(t, "internal/*.go", activated[0].MatchedGlob)
	assert.Equal(t, "Follow the Go rules skill.", activated[0].Instructions)
}
