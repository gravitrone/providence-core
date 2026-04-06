package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupEditFile(t *testing.T, content string) (string, *FileState) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	fs := NewFileState()
	fs.MarkRead(path)
	return path, fs
}

func TestEdit_SingleReplacement(t *testing.T) {
	path, fs := setupEditFile(t, "hello world")
	e := NewEditTool(fs)

	res := e.Execute(context.Background(), map[string]any{
		"file_path":  path,
		"old_string": "hello",
		"new_string": "goodbye",
	})

	assert.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "Replaced 1 occurrence")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "goodbye world", string(data))
}

func TestEdit_MultipleMatchesWithoutReplaceAll(t *testing.T) {
	path, fs := setupEditFile(t, "aaa bbb aaa")
	e := NewEditTool(fs)

	res := e.Execute(context.Background(), map[string]any{
		"file_path":  path,
		"old_string": "aaa",
		"new_string": "ccc",
	})

	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "2 times")
}

func TestEdit_ReplaceAll(t *testing.T) {
	path, fs := setupEditFile(t, "aaa bbb aaa")
	e := NewEditTool(fs)

	res := e.Execute(context.Background(), map[string]any{
		"file_path":   path,
		"old_string":  "aaa",
		"new_string":  "ccc",
		"replace_all": true,
	})

	assert.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "Replaced 2 occurrences")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "ccc bbb ccc", string(data))
}

func TestEdit_NotFound(t *testing.T) {
	path, fs := setupEditFile(t, "hello world")
	e := NewEditTool(fs)

	res := e.Execute(context.Background(), map[string]any{
		"file_path":  path,
		"old_string": "missing",
		"new_string": "replacement",
	})

	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "not found")
}

func TestEdit_NotReadFirst(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unread.txt")
	require.NoError(t, os.WriteFile(path, []byte("content"), 0o644))

	fs := NewFileState()
	e := NewEditTool(fs)

	res := e.Execute(context.Background(), map[string]any{
		"file_path":  path,
		"old_string": "content",
		"new_string": "new",
	})

	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "not been read")
}

func TestEdit_StaleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stale.txt")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0o644))

	fs := NewFileState()
	fs.MarkRead(path)

	// Modify the file behind our back (change mtime).
	require.NoError(t, os.WriteFile(path, []byte("modified"), 0o644))

	e := NewEditTool(fs)
	res := e.Execute(context.Background(), map[string]any{
		"file_path":  path,
		"old_string": "modified",
		"new_string": "new",
	})

	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "modified since last read")
}

func TestEdit_IdenticalStrings(t *testing.T) {
	path, fs := setupEditFile(t, "hello")
	e := NewEditTool(fs)

	res := e.Execute(context.Background(), map[string]any{
		"file_path":  path,
		"old_string": "hello",
		"new_string": "hello",
	})

	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "identical")
}

func TestEdit_MissingParams(t *testing.T) {
	fs := NewFileState()
	e := NewEditTool(fs)

	res := e.Execute(context.Background(), map[string]any{})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "file_path is required")

	res = e.Execute(context.Background(), map[string]any{
		"file_path": "/tmp/x",
	})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "old_string is required")
}
