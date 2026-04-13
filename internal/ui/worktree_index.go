package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// WorktreeIndexEntry represents a single file in the worktree index.
type WorktreeIndexEntry struct {
	Path string `json:"path"`
	Desc string `json:"desc,omitempty"`
}

// WorktreeIndex is the full index written to .providence/worktree-index.json.
type WorktreeIndex struct {
	Files []WorktreeIndexEntry `json:"files"`
	Total int                  `json:"total"`
}

// indexWorktreeMsg carries the result of a /index command.
type indexWorktreeMsg struct {
	result indexResult
}

type indexResult struct {
	Index WorktreeIndex
	Path  string
	Err   error
}

// runWorktreeIndex runs git ls-files, builds a worktree index, and writes
// it to .providence/worktree-index.json.
func runWorktreeIndex() indexResult {
	cwd, err := os.Getwd()
	if err != nil {
		return indexResult{Err: err}
	}

	// Run git ls-files to get tracked files.
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return indexResult{Err: err}
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var entries []WorktreeIndexEntry
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entries = append(entries, WorktreeIndexEntry{
			Path: line,
			Desc: inferFileDesc(line),
		})
	}

	// Sort by path for consistency.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})

	index := WorktreeIndex{
		Files: entries,
		Total: len(entries),
	}

	// Write to .providence/worktree-index.json
	destDir := filepath.Join(cwd, ".providence")
	_ = os.MkdirAll(destDir, 0o755)
	destPath := filepath.Join(destDir, "worktree-index.json")

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return indexResult{Err: err}
	}
	if err := os.WriteFile(destPath, data, 0o644); err != nil {
		return indexResult{Err: err}
	}

	return indexResult{
		Index: index,
		Path:  destPath,
	}
}

// inferFileDesc returns a brief description based on the file name/extension.
func inferFileDesc(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)

	// Known files.
	switch base {
	case "go.mod":
		return "Go module definition"
	case "go.sum":
		return "Go dependency checksums"
	case "package.json":
		return "Node.js package manifest"
	case "Cargo.toml":
		return "Rust crate manifest"
	case "Makefile":
		return "Build automation"
	case "Dockerfile":
		return "Container build"
	case "README.md":
		return "Project readme"
	case "CLAUDE.md":
		return "Claude Code instructions"
	case ".gitignore":
		return "Git ignore rules"
	case "config.toml":
		return "Configuration"
	}

	// Test files.
	if strings.HasSuffix(base, "_test.go") {
		return "Go test"
	}
	if strings.HasSuffix(base, ".test.ts") || strings.HasSuffix(base, ".test.js") || strings.HasSuffix(base, ".spec.ts") {
		return "Test"
	}

	// Extension-based.
	switch ext {
	case ".go":
		return "Go source"
	case ".ts", ".tsx":
		return "TypeScript"
	case ".js", ".jsx":
		return "JavaScript"
	case ".py":
		return "Python"
	case ".rs":
		return "Rust"
	case ".md":
		return "Markdown"
	case ".yaml", ".yml":
		return "YAML config"
	case ".json":
		return "JSON"
	case ".toml":
		return "TOML config"
	case ".sql":
		return "SQL"
	case ".sh":
		return "Shell script"
	case ".css":
		return "Stylesheet"
	case ".html":
		return "HTML"
	}

	return ""
}

// LoadWorktreeIndex reads .providence/worktree-index.json if it exists.
// Returns nil if the file doesn't exist or fails to parse.
func LoadWorktreeIndex() *WorktreeIndex {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	path := filepath.Join(cwd, ".providence", "worktree-index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var idx WorktreeIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil
	}
	return &idx
}

// TopWorktreeFiles returns up to n files from the worktree index,
// formatted as a system prompt injection.
func TopWorktreeFiles(n int) string {
	idx := LoadWorktreeIndex()
	if idx == nil || len(idx.Files) == 0 {
		return ""
	}

	limit := n
	if limit > len(idx.Files) {
		limit = len(idx.Files)
	}

	var b strings.Builder
	b.WriteString("## Worktree Index (top files)\n\n")
	for _, f := range idx.Files[:limit] {
		if f.Desc != "" {
			b.WriteString("- `" + f.Path + "` - " + f.Desc + "\n")
		} else {
			b.WriteString("- `" + f.Path + "`\n")
		}
	}
	if idx.Total > limit {
		b.WriteString(fmt.Sprintf("\n... and %d more files\n", idx.Total-limit))
	}

	return b.String()
}
