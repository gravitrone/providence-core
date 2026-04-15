package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGlobBasic(t *testing.T) {
	gt := NewGlobTool()
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "a.go"), []byte("pkg"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "b.go"), []byte("pkg"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "c.txt"), []byte("hi"), 0644))

	res := gt.Execute(context.Background(), map[string]any{
		"pattern": "*.go",
		"path":    tmp,
	})
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "a.go")
	assert.Contains(t, res.Content, "b.go")
	assert.NotContains(t, res.Content, "c.txt")
}

func TestGlobSortByMtime(t *testing.T) {
	gt := NewGlobTool()
	tmp := t.TempDir()

	// create files with different mtimes
	p1 := filepath.Join(tmp, "old.go")
	p2 := filepath.Join(tmp, "new.go")
	require.NoError(t, os.WriteFile(p1, []byte("old"), 0644))
	require.NoError(t, os.WriteFile(p2, []byte("new"), 0644))

	old := time.Now().Add(-10 * time.Second)
	require.NoError(t, os.Chtimes(p1, old, old))

	res := gt.Execute(context.Background(), map[string]any{
		"pattern": "*.go",
		"path":    tmp,
	})
	assert.False(t, res.IsError)
	lines := strings.Split(strings.TrimSpace(res.Content), "\n")
	require.Len(t, lines, 2)
	// new.go should come first (most recent)
	assert.Contains(t, lines[0], "new.go")
	assert.Contains(t, lines[1], "old.go")
}

func TestGlobExcludesGitDir(t *testing.T) {
	gt := NewGlobTool()
	tmp := t.TempDir()

	gitDir := filepath.Join(tmp, ".git")
	require.NoError(t, os.MkdirAll(gitDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "config.go"), []byte("x"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "main.go"), []byte("x"), 0644))

	res := gt.Execute(context.Background(), map[string]any{
		"pattern": "*.go",
		"path":    tmp,
	})
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "main.go")
	assert.NotContains(t, res.Content, ".git")
}

func TestGlobExcludesNodeModules(t *testing.T) {
	gt := NewGlobTool()
	tmp := t.TempDir()

	nm := filepath.Join(tmp, "node_modules")
	require.NoError(t, os.MkdirAll(nm, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(nm, "lib.go"), []byte("x"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "app.go"), []byte("x"), 0644))

	res := gt.Execute(context.Background(), map[string]any{
		"pattern": "*.go",
		"path":    tmp,
	})
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "app.go")
	assert.NotContains(t, res.Content, "node_modules")
}

func TestGlobExcludesDSStore(t *testing.T) {
	gt := NewGlobTool()
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".DS_Store"), []byte("x"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("x"), 0644))

	res := gt.Execute(context.Background(), map[string]any{
		"pattern": "*",
		"path":    tmp,
	})
	assert.False(t, res.IsError)
	assert.NotContains(t, res.Content, ".DS_Store")
}

func TestGlobMissingPattern(t *testing.T) {
	gt := NewGlobTool()
	res := gt.Execute(context.Background(), map[string]any{})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "pattern is required")
}

func TestGlobSubdirectories(t *testing.T) {
	gt := NewGlobTool()
	tmp := t.TempDir()

	sub := filepath.Join(tmp, "sub")
	require.NoError(t, os.MkdirAll(sub, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "deep.go"), []byte("x"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "top.go"), []byte("x"), 0644))

	res := gt.Execute(context.Background(), map[string]any{
		"pattern": "**/*.go",
		"path":    tmp,
	})
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "deep.go")
	assert.Contains(t, res.Content, "top.go")
}

func TestGlobNoMatches(t *testing.T) {
	gt := NewGlobTool()
	tmp := t.TempDir()

	res := gt.Execute(context.Background(), map[string]any{
		"pattern": "*.xyz",
		"path":    tmp,
	})
	assert.False(t, res.IsError)
	assert.Equal(t, "", res.Content)
}

// TestGlobStarStarMatchesArbitraryDepth verifies the real correctness
// fix: patterns like `src/**/*.ts` must match files at depth 1, 2, 3+
// under src, and must NOT match files outside src. The previous
// implementation only matched the base-name pattern against each entry
// which produced false positives for sibling dirs and false negatives
// for deep nesting.
func TestGlobStarStarMatchesArbitraryDepth(t *testing.T) {
	gt := NewGlobTool()
	tmp := t.TempDir()

	// src/a.ts (depth 1), src/sub/b.ts (depth 2), src/sub/deep/c.ts (depth 3).
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "src", "sub", "deep"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "src", "a.ts"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "src", "sub", "b.ts"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "src", "sub", "deep", "c.ts"), []byte("x"), 0o644))

	// Sibling dir that must NOT be included.
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "other"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "other", "nope.ts"), []byte("x"), 0o644))

	// Non-ts file inside src must also be filtered out.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "src", "keep.go"), []byte("x"), 0o644))

	res := gt.Execute(context.Background(), map[string]any{
		"pattern": "src/**/*.ts",
		"path":    tmp,
	})
	require.False(t, res.IsError, "walk must succeed: %s", res.Content)

	body := res.Content
	assert.Contains(t, body, filepath.Join("src", "a.ts"), "depth-1 match missing")
	assert.Contains(t, body, filepath.Join("src", "sub", "b.ts"), "depth-2 match missing")
	assert.Contains(t, body, filepath.Join("src", "sub", "deep", "c.ts"), "depth-3 match missing")
	assert.NotContains(t, body, filepath.Join("other", "nope.ts"),
		"sibling tree must not leak through a **/ prefix rooted at src/")
	assert.NotContains(t, body, "keep.go",
		"non-ts file in src must be filtered by the *.ts suffix")
}

// TestGlobTopLevelStarStarMatchesRoot verifies a pattern whose ** is
// the entire path prefix matches files in the root directory too.
// doublestar semantics: `**/*.go` includes `foo.go` at the walk root.
func TestGlobTopLevelStarStarMatchesRoot(t *testing.T) {
	gt := NewGlobTool()
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "root.go"), []byte("x"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "nested", "sub.go"), []byte("x"), 0o644))

	res := gt.Execute(context.Background(), map[string]any{
		"pattern": "**/*.go",
		"path":    tmp,
	})
	require.False(t, res.IsError)
	assert.Contains(t, res.Content, "root.go", "**/*.go must match files at the walk root")
	assert.Contains(t, res.Content, filepath.Join("nested", "sub.go"))
}

// TestGlobInvalidPatternReturnsError verifies doublestar.ValidatePattern
// catches syntactically broken patterns up front instead of silently
// producing zero results.
func TestGlobInvalidPatternReturnsError(t *testing.T) {
	gt := NewGlobTool()
	tmp := t.TempDir()

	res := gt.Execute(context.Background(), map[string]any{
		"pattern": "[unclosed",
		"path":    tmp,
	})
	assert.True(t, res.IsError, "malformed pattern must surface as an error")
	assert.Contains(t, res.Content, "invalid glob pattern")
}

// TestGlobBraceExpansionMultipleExtensions verifies brace syntax
// (supported by doublestar/v4) so users can glob multiple extensions
// in a single pattern - a notable UX win over the old impl.
func TestGlobBraceExpansionMultipleExtensions(t *testing.T) {
	gt := NewGlobTool()
	tmp := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "src", "a.go"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "src", "b.ts"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "src", "c.py"), []byte("x"), 0o644))

	res := gt.Execute(context.Background(), map[string]any{
		"pattern": "src/*.{go,ts}",
		"path":    tmp,
	})
	require.False(t, res.IsError)
	assert.Contains(t, res.Content, "a.go")
	assert.Contains(t, res.Content, "b.ts")
	assert.NotContains(t, res.Content, "c.py", "py file must not be picked up by {go,ts} alternation")
}

// TestGlobQuestionMarkMatchesSingleChar verifies the ? metacharacter
// matches exactly one character, round-tripped through the doublestar
// path.
func TestGlobQuestionMarkMatchesSingleChar(t *testing.T) {
	gt := NewGlobTool()
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "a1.go"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "a12.go"), []byte("x"), 0o644))

	res := gt.Execute(context.Background(), map[string]any{
		"pattern": "a?.go",
		"path":    tmp,
	})
	require.False(t, res.IsError)
	assert.Contains(t, res.Content, "a1.go", "single-char ? must match")
	assert.NotContains(t, res.Content, "a12.go", "? matches exactly one char, not many")
}
