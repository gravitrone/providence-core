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
