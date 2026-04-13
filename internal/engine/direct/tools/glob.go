package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxGlobResults = 100

// skipDirs are directory names to exclude from glob results.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
}

// skipFiles are file names to exclude from glob results.
var skipFiles = map[string]bool{
	".DS_Store": true,
}

// GlobTool finds files matching a glob pattern, sorted by mtime (most recent first).
type GlobTool struct{}

func NewGlobTool() *GlobTool    { return &GlobTool{} }
func (g *GlobTool) Name() string { return "Glob" }
func (g *GlobTool) Description() string {
	return "Find files matching a glob pattern, sorted by modification time."
}
func (g *GlobTool) ReadOnly() bool { return true }

// Prompt implements ToolPrompter with CC-parity guidance for glob matching.
func (g *GlobTool) Prompt() string {
	return `Fast file pattern matching tool that works with any codebase size.
- Supports glob patterns like "**/*.js" or "src/**/*.ts"
- Returns matching file paths sorted by modification time
- Use this tool when you need to find files by name patterns
- When you are doing an open ended search that may require multiple rounds of globbing and grepping, use the Agent tool instead`
}

func (g *GlobTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Glob pattern to match (e.g. \"**/*.go\").",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Root directory to search in (defaults to \".\").",
			},
		},
		"required": []string{"pattern"},
	}
}

func (g *GlobTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	pattern := paramString(input, "pattern", "")
	if pattern == "" {
		return ToolResult{Content: "pattern is required", IsError: true}
	}

	root := paramString(input, "path", ".")
	root = filepath.Clean(root)

	// If pattern is relative, join with root
	fullPattern := pattern
	if !filepath.IsAbs(pattern) {
		fullPattern = filepath.Join(root, pattern)
	}

	// Walk-based glob to support ** patterns.
	// Hard limits to prevent hanging on large directories.
	type fileEntry struct {
		path  string
		mtime int64
	}

	var results []fileEntry
	truncated := false
	filesWalked := 0
	const maxFilesWalked = 10000 // stop walking after 10k files regardless of matches

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		// Respect context cancellation.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil // skip inaccessible files
		}

		// skip excluded directories
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		filesWalked++
		if filesWalked > maxFilesWalked {
			truncated = true
			return filepath.SkipAll
		}

		// skip excluded files
		if skipFiles[info.Name()] {
			return nil
		}

		// match against pattern
		matched, matchErr := filepath.Match(filepath.Base(fullPattern), info.Name())
		if matchErr != nil {
			return fmt.Errorf("invalid glob pattern %q: %w", fullPattern, matchErr)
		}

		// For patterns with directory components, also try matching the full path
		if !matched {
			if m, err := filepath.Match(fullPattern, path); err == nil {
				matched = m
			}
		}

		// For ** patterns, match just the base name against the last component
		if !matched && strings.Contains(pattern, "**") {
			parts := strings.Split(pattern, "/")
			lastPart := parts[len(parts)-1]
			if m, err := filepath.Match(lastPart, info.Name()); err == nil {
				matched = m
			}
		}

		if matched {
			results = append(results, fileEntry{path: path, mtime: info.ModTime().UnixNano()})
		}
		return nil
	})

	if err != nil {
		return ToolResult{Content: fmt.Sprintf("walk error: %v", err), IsError: true}
	}

	// sort by mtime descending (most recent first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].mtime > results[j].mtime
	})

	if len(results) > maxGlobResults {
		results = results[:maxGlobResults]
		truncated = true
	}

	var b strings.Builder
	for _, r := range results {
		b.WriteString(r.path)
		b.WriteByte('\n')
	}

	if truncated {
		b.WriteString(fmt.Sprintf("\n(truncated to %d results)\n", maxGlobResults))
	}

	return ToolResult{Content: b.String()}
}
