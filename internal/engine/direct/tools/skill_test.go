package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSkillToolName(t *testing.T) {
	st := NewSkillTool()
	if st.Name() != "Skill" {
		t.Fatalf("expected Name() == \"Skill\", got %q", st.Name())
	}
}

func TestSkillToolSchema(t *testing.T) {
	st := NewSkillTool()
	schema := st.InputSchema()
	if schema["type"] != "object" {
		t.Fatalf("expected schema type \"object\", got %v", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties map")
	}
	if _, ok := props["skill"]; !ok {
		t.Error("expected 'skill' property in schema")
	}
	if _, ok := props["args"]; !ok {
		t.Error("expected 'args' property in schema")
	}
	req, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("expected required []string")
	}
	if len(req) != 1 || req[0] != "skill" {
		t.Fatalf("expected required=[\"skill\"], got %v", req)
	}
}

func TestSkillToolReadOnly(t *testing.T) {
	st := NewSkillTool()
	if !st.ReadOnly() {
		t.Fatal("SkillTool should be read-only")
	}
}

func TestSkillToolNotFound(t *testing.T) {
	// Empty dirs - no skills available.
	st := NewSkillToolWithDirs([]string{})
	result := st.Execute(context.Background(), map[string]any{"skill": "nonexistent"})
	if !result.IsError {
		t.Fatal("expected error for missing skill")
	}
}

func TestSkillToolNotFoundListsAvailable(t *testing.T) {
	dir := t.TempDir()
	// Write a skill file.
	if err := os.WriteFile(filepath.Join(dir, "my-skill.md"), []byte("do the thing"), 0o644); err != nil {
		t.Fatal(err)
	}

	st := NewSkillToolWithDirs([]string{dir})
	result := st.Execute(context.Background(), map[string]any{"skill": "nonexistent"})
	if !result.IsError {
		t.Fatal("expected error for missing skill")
	}
	if result.Content == "" {
		t.Fatal("expected error message listing available skills")
	}
	// Should mention the available skill.
	if !containsString(result.Content, "my-skill") {
		t.Fatalf("expected available skill listed in error, got: %s", result.Content)
	}
}

func TestSkillToolFound(t *testing.T) {
	dir := t.TempDir()
	content := "# My Skill\n\nDo the thing."
	if err := os.WriteFile(filepath.Join(dir, "my-skill.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	st := NewSkillToolWithDirs([]string{dir})
	result := st.Execute(context.Background(), map[string]any{"skill": "my-skill"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != content {
		t.Fatalf("expected skill content %q, got %q", content, result.Content)
	}
}

func TestSkillToolFoundWithArgs(t *testing.T) {
	dir := t.TempDir()
	content := "Do the thing."
	if err := os.WriteFile(filepath.Join(dir, "my-skill.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	st := NewSkillToolWithDirs([]string{dir})
	result := st.Execute(context.Background(), map[string]any{"skill": "my-skill", "args": "arg1 arg2"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !containsString(result.Content, "arg1 arg2") {
		t.Fatalf("expected args in result, got: %s", result.Content)
	}
	if !containsString(result.Content, content) {
		t.Fatalf("expected skill content in result, got: %s", result.Content)
	}
}

func TestSkillToolCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "MySkill.md"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	st := NewSkillToolWithDirs([]string{dir})
	result := st.Execute(context.Background(), map[string]any{"skill": "myskill"})
	if result.IsError {
		t.Fatalf("expected case-insensitive match, got error: %s", result.Content)
	}
}

func TestSkillToolMissingParam(t *testing.T) {
	st := NewSkillTool()
	result := st.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Fatal("expected error when skill param missing")
	}
}

// TestSkillToolEmptyStringSkillParam pins the empty-value branch
// distinct from the missing-key branch already covered above. A caller
// that passes {"skill": ""} must get a clean error, not a spurious
// directory listing or a panic.
func TestSkillToolEmptyStringSkillParam(t *testing.T) {
	st := NewSkillTool()
	result := st.Execute(context.Background(), map[string]any{"skill": ""})
	if !result.IsError {
		t.Fatal("expected error when skill param is empty string")
	}
}

// TestSkillToolWhitespaceSkillParam verifies whitespace-only input is
// rejected symmetrically with empty input. Otherwise a typo like
// "skill:   " would silently fail or match an unrelated skill.
func TestSkillToolWhitespaceSkillParam(t *testing.T) {
	st := NewSkillTool()
	for _, input := range []string{"   ", "\t", "\n", " \t \n "} {
		result := st.Execute(context.Background(), map[string]any{"skill": input})
		if !result.IsError {
			t.Fatalf("expected error for whitespace-only input %q", input)
		}
	}
}

// containsString is a helper since strings.Contains is fine but keeps test readable.
func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
