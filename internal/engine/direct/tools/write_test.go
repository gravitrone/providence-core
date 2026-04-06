package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrite_NewFile(t *testing.T) {
	dir := t.TempDir()
	fs := NewFileState()
	w := NewWriteTool(fs)

	path := filepath.Join(dir, "hello.txt")
	res := w.Execute(context.Background(), map[string]any{
		"file_path": path,
		"content":   "hello world",
	})

	assert.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "Created")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(data))
}

func TestWrite_CreatesIntermediateDirectories(t *testing.T) {
	dir := t.TempDir()
	fs := NewFileState()
	w := NewWriteTool(fs)

	path := filepath.Join(dir, "a", "b", "c", "file.txt")
	res := w.Execute(context.Background(), map[string]any{
		"file_path": path,
		"content":   "nested",
	})

	assert.False(t, res.IsError, res.Content)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "nested", string(data))
}

func TestWrite_ExistingFileRequiresRead(t *testing.T) {
	dir := t.TempDir()
	fs := NewFileState()
	w := NewWriteTool(fs)

	path := filepath.Join(dir, "existing.txt")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))

	// Write without reading first should fail.
	res := w.Execute(context.Background(), map[string]any{
		"file_path": path,
		"content":   "new",
	})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "not been read")

	// After marking as read, write should succeed.
	fs.MarkRead(path)
	res = w.Execute(context.Background(), map[string]any{
		"file_path": path,
		"content":   "new",
	})
	assert.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "Updated")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))
}

func TestWrite_MissingFilePath(t *testing.T) {
	fs := NewFileState()
	w := NewWriteTool(fs)

	res := w.Execute(context.Background(), map[string]any{
		"content": "stuff",
	})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "file_path is required")
}

func TestWrite_EmptyContent(t *testing.T) {
	dir := t.TempDir()
	fs := NewFileState()
	w := NewWriteTool(fs)

	path := filepath.Join(dir, "empty.txt")
	res := w.Execute(context.Background(), map[string]any{
		"file_path": path,
		"content":   "",
	})
	assert.False(t, res.IsError, res.Content)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "", string(data))
}
