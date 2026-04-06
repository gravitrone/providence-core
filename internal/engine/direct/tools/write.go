package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// WriteTool creates or overwrites files with read-before-write safety.
type WriteTool struct {
	fs *FileState
}

// NewWriteTool creates a WriteTool backed by the given FileState.
func NewWriteTool(fs *FileState) *WriteTool {
	return &WriteTool{fs: fs}
}

func (w *WriteTool) Name() string        { return "Write" }
func (w *WriteTool) Description() string { return "Write content to a file, creating directories as needed." }
func (w *WriteTool) ReadOnly() bool      { return false }

func (w *WriteTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "Absolute path to the file to write.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write to the file.",
			},
		},
		"required": []string{"file_path", "content"},
	}
}

func (w *WriteTool) Execute(_ context.Context, input map[string]any) ToolResult {
	path := paramString(input, "file_path", "")
	content := paramString(input, "content", "")

	if path == "" {
		return ToolResult{Content: "file_path is required", IsError: true}
	}

	// Check if file already exists.
	_, statErr := os.Stat(path)
	fileExists := statErr == nil

	// For existing files, require a prior read.
	if fileExists && !w.fs.HasBeenRead(path) {
		return ToolResult{
			Content: fmt.Sprintf("file %s exists but has not been read first", path),
			IsError: true,
		}
	}

	// Ensure parent directories exist.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to create directories: %v", err), IsError: true}
	}

	// Atomic write: write to temp file in the same directory, then rename.
	tmp, err := os.CreateTemp(dir, ".write-tmp-*")
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to create temp file: %v", err), IsError: true}
	}
	tmpName := tmp.Name()

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return ToolResult{Content: fmt.Sprintf("failed to write temp file: %v", err), IsError: true}
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return ToolResult{Content: fmt.Sprintf("failed to close temp file: %v", err), IsError: true}
	}

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return ToolResult{Content: fmt.Sprintf("failed to rename temp file: %v", err), IsError: true}
	}

	// Update file state so subsequent edits see this write.
	w.fs.MarkRead(path)

	verb := "Created"
	if fileExists {
		verb = "Updated"
	}
	return ToolResult{Content: fmt.Sprintf("%s %s", verb, path)}
}
