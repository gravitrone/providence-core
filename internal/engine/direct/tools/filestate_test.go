package tools

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileStateMarkAndHasBeenRead(t *testing.T) {
	fs := NewFileState()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "a.txt")
	require.NoError(t, os.WriteFile(p, []byte("hi"), 0644))

	assert.False(t, fs.HasBeenRead(p))
	fs.MarkRead(p)
	assert.True(t, fs.HasBeenRead(p))
}

func TestFileStateCheckStaleUnchanged(t *testing.T) {
	fs := NewFileState()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "a.txt")
	require.NoError(t, os.WriteFile(p, []byte("hi"), 0644))

	fs.MarkRead(p)
	assert.False(t, fs.CheckStale(p), "file unchanged, should not be stale")
}

func TestFileStateCheckStaleAfterModification(t *testing.T) {
	fs := NewFileState()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "a.txt")
	require.NoError(t, os.WriteFile(p, []byte("v1"), 0644))

	fs.MarkRead(p)

	// ensure mtime advances (some filesystems have 1s resolution)
	time.Sleep(50 * time.Millisecond)
	now := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(p, now, now))

	assert.True(t, fs.CheckStale(p), "file was modified, should be stale")
}

func TestFileStateCheckStaleFileDeleted(t *testing.T) {
	fs := NewFileState()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "a.txt")
	require.NoError(t, os.WriteFile(p, []byte("bye"), 0644))

	fs.MarkRead(p)
	require.NoError(t, os.Remove(p))

	assert.True(t, fs.CheckStale(p), "file deleted, should be stale")
}

func TestFileStateCheckStaleNeverRead(t *testing.T) {
	fs := NewFileState()
	assert.False(t, fs.CheckStale("/nonexistent"), "never read, should return false not stale")
}

func TestFileStateMarkReadNonexistent(t *testing.T) {
	fs := NewFileState()
	fs.MarkRead("/does/not/exist")
	assert.True(t, fs.HasBeenRead("/does/not/exist"))
}
