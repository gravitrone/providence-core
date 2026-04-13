package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupGrepDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "main.go"), []byte(
		"package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
	), 0644))

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "util.go"), []byte(
		"package main\n\nfunc helper() string {\n\treturn \"world\"\n}\n",
	), 0644))

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "readme.txt"), []byte(
		"This is a readme.\nNo code here.\n",
	), 0644))

	return tmp
}

func TestGrepFilesWithMatches(t *testing.T) {
	gt := NewGrepTool()
	tmp := setupGrepDir(t)

	res := gt.Execute(context.Background(), map[string]any{
		"pattern": "func",
		"path":    tmp,
	})
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "main.go")
	assert.Contains(t, res.Content, "util.go")
	assert.NotContains(t, res.Content, "readme.txt")
}

func TestGrepContentMode(t *testing.T) {
	gt := NewGrepTool()
	tmp := setupGrepDir(t)

	res := gt.Execute(context.Background(), map[string]any{
		"pattern":     "Println",
		"path":        tmp,
		"output_mode": "content",
	})
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "Println")
	assert.Contains(t, res.Content, ":4:") // line number
}

func TestGrepCountMode(t *testing.T) {
	gt := NewGrepTool()
	tmp := setupGrepDir(t)

	res := gt.Execute(context.Background(), map[string]any{
		"pattern":     "func",
		"path":        tmp,
		"output_mode": "count",
	})
	assert.False(t, res.IsError)
	// each .go file has 1 func
	lines := strings.Split(strings.TrimSpace(res.Content), "\n")
	assert.Len(t, lines, 2)
	for _, line := range lines {
		assert.Contains(t, line, ":1")
	}
}

func TestGrepWithGlobFilter(t *testing.T) {
	gt := NewGrepTool()
	tmp := setupGrepDir(t)

	res := gt.Execute(context.Background(), map[string]any{
		"pattern": ".*",
		"path":    tmp,
		"glob":    "*.go",
	})
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "main.go")
	assert.Contains(t, res.Content, "util.go")
	assert.NotContains(t, res.Content, "readme.txt")
}

func TestGrepInvalidRegex(t *testing.T) {
	gt := NewGrepTool()
	res := gt.Execute(context.Background(), map[string]any{
		"pattern": "[invalid",
		"path":    ".",
	})
	assert.True(t, res.IsError)
	// rg reports "regex parse error", Go fallback reports "invalid regex"
	assert.True(t, strings.Contains(res.Content, "invalid regex") || strings.Contains(res.Content, "regex parse error"),
		"expected regex error message, got: %s", res.Content)
}

func TestGrepMissingPattern(t *testing.T) {
	gt := NewGrepTool()
	res := gt.Execute(context.Background(), map[string]any{})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "pattern is required")
}

func TestGrepSkipsGitDir(t *testing.T) {
	gt := NewGrepTool()
	tmp := t.TempDir()

	gitDir := filepath.Join(tmp, ".git")
	require.NoError(t, os.MkdirAll(gitDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "config"), []byte("match me"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "real.txt"), []byte("match me"), 0644))

	res := gt.Execute(context.Background(), map[string]any{
		"pattern": "match",
		"path":    tmp,
	})
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "real.txt")
	assert.NotContains(t, res.Content, ".git")
}

func TestGrepSkipsBinaryFiles(t *testing.T) {
	gt := NewGrepTool()
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "bin.dat"), []byte{0x00, 0x01, 0x02}, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "text.txt"), []byte("searchable"), 0644))

	res := gt.Execute(context.Background(), map[string]any{
		"pattern": ".",
		"path":    tmp,
	})
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "text.txt")
	assert.NotContains(t, res.Content, "bin.dat")
}

func TestGrepHeadLimit(t *testing.T) {
	gt := NewGrepTool()
	tmp := t.TempDir()

	// create many files that match
	for i := 0; i < 10; i++ {
		name := filepath.Join(tmp, strings.Repeat("a", i+1)+".txt")
		require.NoError(t, os.WriteFile(name, []byte("findme"), 0644))
	}

	res := gt.Execute(context.Background(), map[string]any{
		"pattern":    "findme",
		"path":       tmp,
		"head_limit": float64(3),
	})
	assert.False(t, res.IsError)
	lines := strings.Split(strings.TrimSpace(res.Content), "\n")
	assert.Len(t, lines, 3)
}

func TestGrepNoMatches(t *testing.T) {
	gt := NewGrepTool()
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("hello"), 0644))

	res := gt.Execute(context.Background(), map[string]any{
		"pattern": "zzz_no_match",
		"path":    tmp,
	})
	assert.False(t, res.IsError)
	assert.Equal(t, "", res.Content)
}

func TestGrepSingleFile(t *testing.T) {
	gt := NewGrepTool()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "single.txt")
	require.NoError(t, os.WriteFile(p, []byte("line1\nline2 target\nline3\n"), 0644))

	res := gt.Execute(context.Background(), map[string]any{
		"pattern":     "target",
		"path":        p,
		"output_mode": "content",
	})
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "target")
	// rg uses "N:line" for single files, Go fallback uses "path:N:line"
	assert.True(t, strings.Contains(res.Content, ":2:") || strings.Contains(res.Content, "2:line2"),
		"expected line 2, got: %s", res.Content)
}

func TestGrepBadPath(t *testing.T) {
	gt := NewGrepTool()
	res := gt.Execute(context.Background(), map[string]any{
		"pattern": "test",
		"path":    "/nonexistent/dir",
	})
	assert.True(t, res.IsError)
	// rg reports "IO error", Go fallback reports "path error"
	assert.True(t, strings.Contains(res.Content, "path error") ||
		strings.Contains(res.Content, "No such file") ||
		strings.Contains(res.Content, "IO error"),
		"expected path error, got: %s", res.Content)
}
