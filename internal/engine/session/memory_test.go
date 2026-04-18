package session

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withTempMemoryDir redirects the memory store to a fresh temp dir for the
// duration of the test and restores the previous override on cleanup.
func withTempMemoryDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev := memoryDirOverride
	SetMemoryDirForTesting(dir)
	t.Cleanup(func() { SetMemoryDirForTesting(prev) })
	return dir
}

func TestMemoryPathIsUnderMemoryDir(t *testing.T) {
	dir := withTempMemoryDir(t)
	path := MemoryPath("abc")
	require.Equal(t, filepath.Join(dir, "abc.md"), path)
}

func TestMemoryPathSanitizesSessionID(t *testing.T) {
	dir := withTempMemoryDir(t)
	path := MemoryPath("../etc/passwd")
	require.True(t, filepath.Dir(path) == dir, "memory path must stay inside memory dir, got %s", path)
	require.NotContains(t, filepath.Base(path), "..")
	require.NotContains(t, filepath.Base(path), string(os.PathSeparator))
}

func TestWriteAndReadSessionMemory(t *testing.T) {
	withTempMemoryDir(t)

	content := "# session memory\n\nuser wants a go tui.\n"
	require.NoError(t, WriteSessionMemory("sess-1", content))

	got, err := ReadSessionMemory("sess-1")
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

func TestReadSessionMemoryMissingReturnsEmpty(t *testing.T) {
	withTempMemoryDir(t)

	got, err := ReadSessionMemory("never-written")
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

func TestWriteSessionMemoryRejectsEmptyID(t *testing.T) {
	withTempMemoryDir(t)

	err := WriteSessionMemory("", "body")
	require.ErrorIs(t, err, ErrEmptySessionID)
}

func TestReadSessionMemoryRejectsEmptyID(t *testing.T) {
	withTempMemoryDir(t)

	_, err := ReadSessionMemory("   ")
	require.ErrorIs(t, err, ErrEmptySessionID)
}

// TestSessionMemoryAtomicWrite verifies that writes do not leave the final
// file truncated if they fail midway. We cannot easily inject a crash, but we
// can verify the two invariants the atomic path must hold:
//
//  1. The final file contains exactly the last successful write, never a
//     partial body from an in-flight write.
//  2. No stray .tmp files are left behind after a successful write.
func TestSessionMemoryAtomicWrite(t *testing.T) {
	dir := withTempMemoryDir(t)

	require.NoError(t, WriteSessionMemory("atomic", "first"))
	require.NoError(t, WriteSessionMemory("atomic", "second-longer-body"))

	got, err := ReadSessionMemory("atomic")
	require.NoError(t, err)
	assert.Equal(t, "second-longer-body", got)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "leftover temp file: %s", e.Name())
	}
}

// TestSessionMemoryConcurrentWritesDoNotCorrupt verifies that parallel writers
// against the same session id produce a readable file containing exactly one
// of the written payloads (not a torn concatenation).
func TestSessionMemoryConcurrentWritesDoNotCorrupt(t *testing.T) {
	withTempMemoryDir(t)

	payloads := []string{"aaaaaa", "bbbbbb", "cccccc", "dddddd"}

	var wg sync.WaitGroup
	for _, p := range payloads {
		wg.Add(1)
		go func(body string) {
			defer wg.Done()
			_ = WriteSessionMemory("race", body)
		}(p)
	}
	wg.Wait()

	got, err := ReadSessionMemory("race")
	require.NoError(t, err)
	found := false
	for _, p := range payloads {
		if p == got {
			found = true
			break
		}
	}
	assert.True(t, found, "final content %q must match one of the written payloads", got)
}

func TestStaleMemoryIsIgnored(t *testing.T) {
	withTempMemoryDir(t)

	require.NoError(t, WriteSessionMemory("old-session", "stale body"))

	// Backdate the file past the stale threshold.
	path := MemoryPath("old-session")
	old := time.Now().Add(-MemoryStaleAfter - time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))

	got, err := ReadSessionMemory("old-session")
	assert.Equal(t, "", got)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMemoryStale))
}

func TestFreshMemoryIsNotStale(t *testing.T) {
	withTempMemoryDir(t)

	require.NoError(t, WriteSessionMemory("fresh", "fresh body"))

	got, err := ReadSessionMemory("fresh")
	require.NoError(t, err)
	assert.Equal(t, "fresh body", got)
}
