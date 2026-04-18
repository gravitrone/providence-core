package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gravitrone/providence-core/internal/engine/hooks"
	"github.com/gravitrone/providence-core/internal/engine/skills"
)

// WriteTool creates or overwrites files with read-before-write safety.
type WriteTool struct {
	fs                     *FileState
	emitter                HookEmitter
	skillActivationHandler func([]skills.ActivatedSkill)
}

// NewWriteTool creates a WriteTool backed by the given FileState.
func NewWriteTool(fs *FileState) *WriteTool {
	return &WriteTool{fs: fs}
}

// SetHookEmitter wires lifecycle hook dispatch for successful writes.
func (w *WriteTool) SetHookEmitter(emitter HookEmitter) {
	w.emitter = emitter
}

// SetSkillActivationHandler wires conditional skill activation for successful writes.
func (w *WriteTool) SetSkillActivationHandler(handler func([]skills.ActivatedSkill)) {
	w.skillActivationHandler = handler
}

func (w *WriteTool) Name() string { return "Write" }
func (w *WriteTool) Description() string {
	return "Write content to a file, creating directories as needed."
}
func (w *WriteTool) ReadOnly() bool { return false }
func (w *WriteTool) ResultSizeCap() int {
	return writeToolResultSizeCap
}

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
			"allow_secrets": map[string]any{
				"type":        "boolean",
				"description": "Bypass the secret-pattern guard for the content. Use only after manual review.",
			},
		},
		"required": []string{"file_path", "content"},
	}
}

func (w *WriteTool) Execute(_ context.Context, input map[string]any) ToolResult {
	path := paramString(input, "file_path", "")
	content := paramString(input, "content", "")
	allowSecrets := paramBool(input, "allow_secrets", false)

	if path == "" {
		return ToolResult{Content: "file_path is required", IsError: true}
	}

	// Size guard fires on the EXISTING file. A fresh create always
	// passes because SizeGuardError returns empty on ENOENT.
	if msg := SizeGuardError(path); msg != "" {
		return ToolResult{Content: msg, IsError: true}
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

	// Secret scanner on the proposed content.
	if !allowSecrets {
		if names := ScanForSecrets(content); len(names) > 0 {
			return ToolResult{Content: FormatSecretsError(names), IsError: true}
		}
	}

	// Settings-file validator: must parse in the target format.
	if err := ValidateSettingsContent(path, content); err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}

	// Snapshot existing content before overwrite so the model can
	// recover via the FileHistory tool. No-op for new files.
	_, _ = SnapshotFile(path)

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
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return ToolResult{Content: fmt.Sprintf("failed to write temp file: %v", err), IsError: true}
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return ToolResult{Content: fmt.Sprintf("failed to close temp file: %v", err), IsError: true}
	}

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return ToolResult{Content: fmt.Sprintf("failed to rename temp file: %v", err), IsError: true}
	}

	// Update file state so subsequent edits see this write.
	w.fs.MarkRead(path)
	w.emitFileChanged(path)

	verb := "Created"
	if fileExists {
		verb = "Updated"
	}
	return ToolResult{
		Content:         fmt.Sprintf("%s %s", verb, path),
		ContextModifier: w.skillActivationModifier(path),
	}
}

func (w *WriteTool) emitFileChanged(path string) {
	if w.emitter == nil {
		return
	}
	w.emitter(hooks.FileChanged, hooks.HookInput{
		ToolName: w.Name(),
		ToolInput: map[string]string{
			"file_path": path,
		},
	})
}

func (w *WriteTool) skillActivationModifier(path string) func() {
	activatedSkills := skills.ActivateForPaths([]string{path})
	if len(activatedSkills) == 0 || w.skillActivationHandler == nil {
		return nil
	}

	return func() {
		w.skillActivationHandler(activatedSkills)
	}
}
