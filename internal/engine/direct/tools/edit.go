package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// EditTool performs string replacements in files with stale-write detection.
type EditTool struct {
	fs *FileState
}

// NewEditTool creates an EditTool backed by the given FileState.
func NewEditTool(fs *FileState) *EditTool {
	return &EditTool{fs: fs}
}

func (e *EditTool) Name() string        { return "Edit" }
func (e *EditTool) Description() string { return "Replace exact strings in an existing file." }
func (e *EditTool) ReadOnly() bool      { return false }

func (e *EditTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "Absolute path to the file to edit.",
			},
			"old_string": map[string]any{
				"type":        "string",
				"description": "The exact text to find and replace.",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "The replacement text.",
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "Replace all occurrences (default false).",
			},
		},
		"required": []string{"file_path", "old_string", "new_string"},
	}
}

func (e *EditTool) Execute(_ context.Context, input map[string]any) ToolResult {
	path := paramString(input, "file_path", "")
	oldStr := paramString(input, "old_string", "")
	newStr := paramString(input, "new_string", "")
	replaceAll := paramBool(input, "replace_all", false)

	if path == "" {
		return ToolResult{Content: "file_path is required", IsError: true}
	}
	if oldStr == "" {
		return ToolResult{Content: "old_string is required", IsError: true}
	}
	if oldStr == newStr {
		return ToolResult{Content: "old_string and new_string are identical", IsError: true}
	}

	// Must have been read first.
	if !e.fs.HasBeenRead(path) {
		return ToolResult{
			Content: fmt.Sprintf("file %s has not been read first", path),
			IsError: true,
		}
	}

	// Check for stale writes.
	if e.fs.CheckStale(path) {
		return ToolResult{
			Content: fmt.Sprintf("file %s has been modified since last read", path),
			IsError: true,
		}
	}

	// Read current content.
	data, err := os.ReadFile(path)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to read file: %v", err), IsError: true}
	}
	content := string(data)

	// Count occurrences.
	count := strings.Count(content, oldStr)
	if count == 0 {
		return ToolResult{Content: "old_string not found in file", IsError: true}
	}
	if count > 1 && !replaceAll {
		return ToolResult{
			Content: fmt.Sprintf("old_string found %d times, use replace_all to replace all occurrences", count),
			IsError: true,
		}
	}

	// Perform replacement.
	var updated string
	if replaceAll {
		updated = strings.ReplaceAll(content, oldStr, newStr)
	} else {
		updated = strings.Replace(content, oldStr, newStr, 1)
	}

	// Write back.
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to write file: %v", err), IsError: true}
	}

	// Update file state so subsequent edits see this write.
	e.fs.MarkRead(path)

	if replaceAll {
		return ToolResult{Content: fmt.Sprintf("Replaced %d occurrences in %s", count, path)}
	}
	return ToolResult{Content: fmt.Sprintf("Replaced 1 occurrence in %s", path)}
}
