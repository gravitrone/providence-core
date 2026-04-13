package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SkillTool lets the model invoke registered skills by name.
// It scans project and user skill directories for .md files and returns
// the skill content so the model can follow its instructions.
type SkillTool struct {
	skillsDirs []string // directories to scan for .md skill files
}

// NewSkillTool creates a SkillTool that scans the default skill directories
// (project .providence/skills/, .claude/commands/, user ~/.providence/skills/).
func NewSkillTool() *SkillTool {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()

	dirs := []string{
		filepath.Join(cwd, ".providence", "skills"),
		filepath.Join(cwd, ".claude", "skills"),
		filepath.Join(cwd, ".claude", "commands"), // CC compat
		filepath.Join(home, ".providence", "skills"),
		filepath.Join(home, ".claude", "skills"),
	}
	return &SkillTool{skillsDirs: dirs}
}

// NewSkillToolWithDirs creates a SkillTool with explicit directories (for testing).
func NewSkillToolWithDirs(dirs []string) *SkillTool {
	return &SkillTool{skillsDirs: dirs}
}

func (s *SkillTool) Name() string { return "Skill" }
func (s *SkillTool) ReadOnly() bool { return true }
func (s *SkillTool) Description() string {
	return "Invoke a registered skill by name. Skills provide specialized workflows for common tasks. Returns the skill instructions for the model to follow."
}

func (s *SkillTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill": map[string]any{
				"type":        "string",
				"description": "Name of the skill to invoke (filename without .md extension).",
			},
			"args": map[string]any{
				"type":        "string",
				"description": "Optional arguments to pass to the skill.",
			},
		},
		"required": []string{"skill"},
	}
}

func (s *SkillTool) Execute(_ context.Context, input map[string]any) ToolResult {
	skillName := paramString(input, "skill", "")
	if skillName == "" {
		return ToolResult{Content: "skill parameter is required", IsError: true}
	}
	args := paramString(input, "args", "")

	// Scan all directories, first match wins (project overrides user).
	var available []string
	seen := make(map[string]struct{})

	for _, dir := range s.skillsDirs {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".md")

			// Track available for error messages.
			if _, exists := seen[name]; !exists {
				seen[name] = struct{}{}
				available = append(available, name)
			}

			// Case-insensitive match.
			if strings.EqualFold(name, skillName) {
				path := filepath.Join(dir, e.Name())
				content, err := os.ReadFile(path)
				if err != nil {
					return ToolResult{
						Content: fmt.Sprintf("failed to read skill %q: %v", skillName, err),
						IsError: true,
					}
				}
				body := string(content)
				if args != "" {
					body = fmt.Sprintf("Arguments: %s\n\n%s", args, body)
				}
				return ToolResult{Content: body}
			}
		}
	}

	// Not found - list available skills.
	if len(available) == 0 {
		return ToolResult{
			Content: fmt.Sprintf("skill %q not found. No skills are currently installed.", skillName),
			IsError: true,
		}
	}
	return ToolResult{
		Content: fmt.Sprintf("skill %q not found. Available skills: %s", skillName, strings.Join(available, ", ")),
		IsError: true,
	}
}
