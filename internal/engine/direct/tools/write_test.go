package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gravitrone/providence-core/internal/engine/hooks"
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

func TestWritePreservesBOMIfRememberedFromRead(t *testing.T) {
	dir := t.TempDir()
	fs := NewFileState()
	readTool := NewReadTool(fs)
	writeTool := NewWriteTool(fs)

	path := filepath.Join(dir, "bom.txt")
	require.NoError(t, os.WriteFile(path, append([]byte{0xEF, 0xBB, 0xBF}, []byte("before\n")...), 0o644))

	readResult := readTool.Execute(context.Background(), map[string]any{"file_path": path})
	require.False(t, readResult.IsError, readResult.Content)

	writeResult := writeTool.Execute(context.Background(), map[string]any{
		"file_path": path,
		"content":   "after\n",
	})
	require.False(t, writeResult.IsError, writeResult.Content)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.True(t, len(data) >= len(utf8BOM))
	assert.Equal(t, utf8BOM, data[:len(utf8BOM)])
	assert.Equal(t, "after\n", string(data[len(utf8BOM):]))
}

func TestWritePreservesCRLFIfRememberedFromRead(t *testing.T) {
	dir := t.TempDir()
	fs := NewFileState()
	readTool := NewReadTool(fs)
	writeTool := NewWriteTool(fs)

	path := filepath.Join(dir, "windows.txt")
	require.NoError(t, os.WriteFile(path, []byte("before\r\nline\r\n"), 0o644))

	readResult := readTool.Execute(context.Background(), map[string]any{"file_path": path})
	require.False(t, readResult.IsError, readResult.Content)

	writeResult := writeTool.Execute(context.Background(), map[string]any{
		"file_path": path,
		"content":   "after\nline\n",
	})
	require.False(t, writeResult.IsError, writeResult.Content)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "after\r\nline\r\n", string(data))
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

func TestFileChangedHookFiresOnWriteAndEdit(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, spy *hookSpy)
	}{
		{
			name: "write",
			run: func(t *testing.T, spy *hookSpy) {
				dir := t.TempDir()
				fs := NewFileState()
				w := NewWriteTool(fs)
				w.SetHookEmitter(spy.record)

				path := filepath.Join(dir, "created.txt")
				res := w.Execute(context.Background(), map[string]any{
					"file_path": path,
					"content":   "created",
				})
				require.False(t, res.IsError, res.Content)
			},
		},
		{
			name: "edit",
			run: func(t *testing.T, spy *hookSpy) {
				path, fs := setupEditFile(t, "before")
				e := NewEditTool(fs)
				e.SetHookEmitter(spy.record)

				res := e.Execute(context.Background(), map[string]any{
					"file_path":  path,
					"old_string": "before",
					"new_string": "after",
				})
				require.False(t, res.IsError, res.Content)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spy := &hookSpy{}
			tt.run(t, spy)

			events, inputs := spy.snapshot()
			require.Equal(t, []string{hooks.FileChanged}, events)
			require.Len(t, inputs, 1)
			assert.Contains(t, []string{"Write", "Edit"}, inputs[0].ToolName)
			payload, ok := inputs[0].ToolInput.(map[string]string)
			require.True(t, ok)
			assert.NotEmpty(t, payload["file_path"])
		})
	}
}
