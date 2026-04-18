package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gravitrone/providence-core/internal/engine/hooks"
	"github.com/gravitrone/providence-core/internal/engine/skills"
)

// EditTool performs string replacements in files with stale-write detection.
type EditTool struct {
	fs                     *FileState
	emitter                HookEmitter
	skillActivationHandler func([]skills.ActivatedSkill)
}

// NewEditTool creates an EditTool backed by the given FileState.
func NewEditTool(fs *FileState) *EditTool {
	return &EditTool{fs: fs}
}

// SetHookEmitter wires lifecycle hook dispatch for successful edits.
func (e *EditTool) SetHookEmitter(emitter HookEmitter) {
	e.emitter = emitter
}

// SetSkillActivationHandler wires conditional skill activation for successful edits.
func (e *EditTool) SetSkillActivationHandler(handler func([]skills.ActivatedSkill)) {
	e.skillActivationHandler = handler
}

func (e *EditTool) Name() string        { return "Edit" }
func (e *EditTool) Description() string { return "Replace exact strings in an existing file." }
func (e *EditTool) ReadOnly() bool      { return false }
func (e *EditTool) ResultSizeCap() int  { return editToolResultSizeCap }

// Prompt implements ToolPrompter with CC-parity guidance for file editing.
func (e *EditTool) Prompt() string {
	return `Performs exact string replacements in files.

Usage:
- You must use your Read tool at least once in the conversation before editing. This tool will error if you attempt an edit without reading the file.
- When editing text from Read tool output, ensure you preserve the exact indentation (tabs/spaces) as it appears AFTER the line number prefix. The line number prefix format is: line number + tab. Everything after that is the actual file content to match. Never include any part of the line number prefix in the old_string or new_string.
- ALWAYS prefer editing existing files in the codebase. NEVER write new files unless explicitly required.
- Only use emojis if the user explicitly requests it. Avoid adding emojis to files unless asked.
- The edit will FAIL if old_string is not unique in the file. Either provide a larger string with more surrounding context to make it unique or use replace_all to change every instance of old_string.
- Use replace_all for replacing and renaming strings across the file. This parameter is useful if you want to rename a variable for instance.`
}

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
			"allow_secrets": map[string]any{
				"type":        "boolean",
				"description": "Bypass the secret-pattern guard for new_string. Use only after manual review.",
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
	allowSecrets := paramBool(input, "allow_secrets", false)

	if path == "" {
		return ToolResult{Content: "file_path is required", IsError: true}
	}
	if oldStr == "" {
		return ToolResult{Content: "old_string is required", IsError: true}
	}
	if oldStr == newStr {
		return ToolResult{Content: "old_string and new_string are identical", IsError: true}
	}

	// Refuse oversized targets up front so we do not spend RAM reading
	// a multi-gigabyte binary just to reject it after the read.
	if msg := SizeGuardError(path); msg != "" {
		return ToolResult{Content: msg, IsError: true}
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

	// Snapshot current content before we rewrite so the model can
	// recover via the FileHistory tool if this edit is wrong.
	_, _ = SnapshotFile(path)

	// Perform replacement.
	var updated string
	if replaceAll {
		updated = strings.ReplaceAll(content, oldStr, newStr)
	} else {
		updated = strings.Replace(content, oldStr, newStr, 1)
	}

	// Secret scanner: inspect only the new_string (what the caller is
	// adding) rather than the whole file so existing secrets in a file
	// the assistant is legitimately editing do not block unrelated
	// changes. allow_secrets=true bypasses for cases where the user
	// really is pasting a credential into a vault file.
	if !allowSecrets {
		if names := ScanForSecrets(newStr); len(names) > 0 {
			return ToolResult{Content: FormatSecretsError(names), IsError: true}
		}
	}

	// Settings-file validator: make sure the new content still parses
	// as the expected format before we overwrite the user's config.
	if err := ValidateSettingsContent(path, updated); err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}

	// Write back.
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to write file: %v", err), IsError: true}
	}

	// Update file state so subsequent edits see this write.
	e.fs.MarkRead(path)
	e.emitFileChanged(path)

	result := ToolResult{
		ContextModifier: e.skillActivationModifier(path),
	}

	if replaceAll {
		result.Content = fmt.Sprintf("Replaced %d occurrences in %s", count, path)
		return result
	}
	result.Content = fmt.Sprintf("Replaced 1 occurrence in %s", path)
	return result
}

func (e *EditTool) emitFileChanged(path string) {
	if e.emitter == nil {
		return
	}
	e.emitter(hooks.FileChanged, hooks.HookInput{
		ToolName: e.Name(),
		ToolInput: map[string]string{
			"file_path": path,
		},
	})
}

func (e *EditTool) skillActivationModifier(path string) func() {
	activatedSkills := skills.ActivateForPaths([]string{path})
	if len(activatedSkills) == 0 || e.skillActivationHandler == nil {
		return nil
	}

	return func() {
		e.skillActivationHandler(activatedSkills)
	}
}
