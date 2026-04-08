package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	original := Config{
		Engine: "direct",
		Model:  "opus",
		Theme:  "night",
	}

	if err := original.SaveTo(path); err != nil {
		t.Fatalf("SaveTo failed: %v", err)
	}

	loaded := LoadFrom(path)
	if loaded.Engine != original.Engine {
		t.Errorf("Engine: got %q, want %q", loaded.Engine, original.Engine)
	}
	if loaded.Model != original.Model {
		t.Errorf("Model: got %q, want %q", loaded.Model, original.Model)
	}
	if loaded.Theme != original.Theme {
		t.Errorf("Theme: got %q, want %q", loaded.Theme, original.Theme)
	}
}

func TestLoadMissingFile(t *testing.T) {
	c := LoadFrom("/nonexistent/path/config.json")
	if c.Engine != "" || c.Model != "" || c.Theme != "" {
		t.Errorf("expected empty config for missing file, got %+v", c)
	}
}

func TestLoadCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte("not json{{{"), 0o644)

	c := LoadFrom(path)
	if c.Engine != "" || c.Model != "" || c.Theme != "" {
		t.Errorf("expected empty config for corrupt file, got %+v", c)
	}
}

func TestSaveCreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "config.json")

	c := Config{Engine: "claude"}
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo with nested dirs failed: %v", err)
	}

	loaded := LoadFrom(path)
	if loaded.Engine != "claude" {
		t.Errorf("Engine: got %q, want %q", loaded.Engine, "claude")
	}
}

func TestSaveOmitsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	c := Config{Engine: "claude"}
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo failed: %v", err)
	}

	data, _ := os.ReadFile(path)
	// Model and Theme should be omitted (omitempty).
	if got := string(data); got != "{\n  \"engine\": \"claude\"\n}\n" {
		t.Errorf("unexpected JSON output:\n%s", got)
	}
}
