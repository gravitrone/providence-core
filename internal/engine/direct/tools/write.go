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

// Prompt implements ToolPrompter with CC-parity guidance for file writing.
func (w *WriteTool) Prompt() string {
	return `Writes a file to the local filesystem.

Usage:
- This tool will overwrite the existing file if there is one at the provided path.
- If this is an existing file, you MUST use the Read tool first to read the file's contents. This tool will fail if you did not read the file first.
- Prefer the Edit tool for modifying existing files - it only sends the diff. Only use this tool to create new files or for complete rewrites.
- NEVER create documentation files (*.md) or README files unless explicitly requested by the User.
- Only use emojis if the user explicitly requests it. Avoid writing emojis to files unless asked.`
}

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
