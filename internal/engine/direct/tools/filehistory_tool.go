package tools

import (
	"context"
	"fmt"
	"strings"
)

// FileHistoryTool exposes list + restore over the on-disk snapshot
// store so a model can recover a file after a botched edit without
// leaving the harness.
type FileHistoryTool struct{}

func NewFileHistoryTool() *FileHistoryTool    { return &FileHistoryTool{} }
func (t *FileHistoryTool) Name() string        { return "FileHistory" }
func (t *FileHistoryTool) Description() string { return "List or restore historical snapshots of a file edited or written during this or prior sessions." }
func (t *FileHistoryTool) ReadOnly() bool      { return false }

func (t *FileHistoryTool) Prompt() string {
	return `FileHistory operates on the gzipped snapshots that Edit and Write record automatically before each write.

Usage:
- operation "list" (default): returns the newest-first snapshot list for the given file_path, with id + timestamp + size.
- operation "restore": restores the file at file_path to the contents of snapshot_id. Overwrites any pending changes.

Snapshots are retained per-path up to 20 entries or 7 days whichever is tighter. Older snapshots are evicted lazily on the next write.

Use FileHistory to recover from a bad Edit (wrong regex, lost state) without asking the user to undo manually.`
}

func (t *FileHistoryTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type":        "string",
				"description": "\"list\" (default) or \"restore\".",
			},
			"file_path": map[string]any{
				"type":        "string",
				"description": "Absolute path of the file whose history to list or restore.",
			},
			"snapshot_id": map[string]any{
				"type":        "string",
				"description": "Required for \"restore\". The snapshot id as returned by \"list\".",
			},
		},
		"required": []string{"file_path"},
	}
}

func (t *FileHistoryTool) Execute(_ context.Context, input map[string]any) ToolResult {
	op := paramString(input, "operation", "list")
	path := paramString(input, "file_path", "")
	if path == "" {
		return ToolResult{Content: "file_path is required", IsError: true}
	}

	switch op {
	case "list":
		snaps, err := ListSnapshots(path)
		if err != nil {
			return ToolResult{Content: fmt.Sprintf("list error: %v", err), IsError: true}
		}
		if len(snaps) == 0 {
			return ToolResult{Content: "no snapshots recorded for this path"}
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Snapshots for %s (newest first):\n", path)
		for _, s := range snaps {
			fmt.Fprintf(&b, "  %s  %s  %d bytes\n", s.ID, s.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"), s.Bytes)
		}
		return ToolResult{Content: b.String()}

	case "restore":
		id := paramString(input, "snapshot_id", "")
		if id == "" {
			return ToolResult{Content: "snapshot_id is required for restore", IsError: true}
		}
		if err := RestoreSnapshot(path, id); err != nil {
			return ToolResult{Content: fmt.Sprintf("restore error: %v", err), IsError: true}
		}
		return ToolResult{Content: fmt.Sprintf("Restored %s from snapshot %s", path, id)}

	default:
		return ToolResult{Content: fmt.Sprintf("unknown operation %q (expected list or restore)", op), IsError: true}
	}
}
