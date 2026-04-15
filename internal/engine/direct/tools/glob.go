package tools

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
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

func NewGlobTool() *GlobTool     { return &GlobTool{} }
func (g *GlobTool) Name() string { return "Glob" }
func (g *GlobTool) Description() string {
	return "Find files matching a glob pattern, sorted by modification time."
}
func (g *GlobTool) ReadOnly() bool { return true }

// Prompt implements ToolPrompter with parity guidance for glob matching.
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

// maxFilesWalked caps the walk to prevent hanging on pathological trees.
const maxFilesWalked = 10_000

// fileEntry is a matched file plus its modification time for ordering.
type fileEntry struct {
	path  string
	mtime int64
}

// Execute walks root and returns files whose relative path matches pattern
// under full globstar semantics (via doublestar/v4). Results are sorted by
// mtime descending and capped at maxGlobResults.
func (g *GlobTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	pattern := paramString(input, "pattern", "")
	if pattern == "" {
		return ToolResult{Content: "pattern is required", IsError: true}
	}

	root := paramString(input, "path", ".")
	root = filepath.Clean(root)

	// If the caller supplied an absolute pattern, split it into a walk
	// root plus a relative pattern. doublestar matches relative paths
	// against the supplied fs root, so we need the pattern to be
	// relative to that root.
	walkRoot := root
	relPattern := pattern
	if filepath.IsAbs(pattern) {
		walkRoot, relPattern = splitAbsolutePattern(pattern)
	}

	// Validate the pattern before walking so we fail fast on bad syntax.
	if !doublestar.ValidatePattern(relPattern) {
		return ToolResult{Content: fmt.Sprintf("invalid glob pattern %q", relPattern), IsError: true}
	}

	results, truncated, err := walkAndMatch(ctx, walkRoot, relPattern)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("walk error: %v", err), IsError: true}
	}

	// Sort by mtime descending (most recent first).
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

// splitAbsolutePattern splits an absolute glob pattern into a concrete
// walk root and a relative pattern for doublestar. For example
// "/repo/src/**/*.go" becomes ("/repo/src", "**/*.go"). We walk from
// the first path component that contains a glob metacharacter.
func splitAbsolutePattern(pattern string) (string, string) {
	parts := strings.Split(pattern, string(filepath.Separator))
	rootParts := []string{}
	for i, p := range parts {
		if strings.ContainsAny(p, "*?[{") {
			remaining := strings.Join(parts[i:], string(filepath.Separator))
			root := strings.Join(rootParts, string(filepath.Separator))
			if root == "" {
				root = string(filepath.Separator)
			}
			return root, remaining
		}
		rootParts = append(rootParts, p)
	}
	// No globs in the pattern - treat the whole thing as a literal path.
	return filepath.Dir(pattern), filepath.Base(pattern)
}

// walkAndMatch walks root and collects files whose path relative to root
// matches pattern. Respects ctx cancellation, skipDirs, skipFiles, and
// the maxFilesWalked cap.
func walkAndMatch(ctx context.Context, root, pattern string) ([]fileEntry, bool, error) {
	var results []fileEntry
	truncated := false
	filesWalked := 0

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil // skip inaccessible entries
		}

		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		filesWalked++
		if filesWalked > maxFilesWalked {
			truncated = true
			return filepath.SkipAll
		}

		if skipFiles[d.Name()] {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		// doublestar uses forward slashes regardless of OS.
		relSlash := filepath.ToSlash(rel)

		matched, matchErr := doublestar.Match(pattern, relSlash)
		if matchErr != nil {
			return fmt.Errorf("match %q against %q: %w", pattern, relSlash, matchErr)
		}
		if !matched {
			return nil
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		results = append(results, fileEntry{path: path, mtime: info.ModTime().UnixNano()})
		return nil
	})

	if err != nil {
		return nil, false, err
	}
	return results, truncated, nil
}
