package filewatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatcher_StartAndStop(t *testing.T) {
	dir := t.TempDir()

	w := New(dir, []string{"test.txt"})
	w.interval = 50 * time.Millisecond
	w.Start()

	// Should be able to stop without hanging.
	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() hung")
	}
}

func TestWatcher_DetectsMtimeChange(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "watched.txt")
	require.NoError(t, os.WriteFile(f, []byte("initial"), 0644))

	w := New(dir, []string{"watched.txt"})
	w.interval = 50 * time.Millisecond
	w.Start()
	defer w.Stop()

	// Wait for initial snapshot to be captured.
	time.Sleep(20 * time.Millisecond)

	// Modify the file - ensure mtime changes.
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, os.WriteFile(f, []byte("modified"), 0644))

	// Wait for the watcher to detect the change.
	select {
	case ev := <-w.Events():
		assert.Equal(t, "watched.txt", ev.Path)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for file change event")
	}
}

func TestWatcher_IgnoresUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "stable.txt")
	require.NoError(t, os.WriteFile(f, []byte("unchanged"), 0644))

	w := New(dir, []string{"stable.txt"})
	w.interval = 50 * time.Millisecond
	w.Start()
	defer w.Stop()

	// Give two poll cycles to complete without any changes.
	time.Sleep(150 * time.Millisecond)

	select {
	case ev := <-w.Events():
		t.Fatalf("unexpected event for unchanged file: %v", ev)
	default:
		// expected - no events
	}
}

func TestWatcher_RecentChangesReturnsAndClears(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "rc.txt")
	require.NoError(t, os.WriteFile(f, []byte("v1"), 0644))

	w := New(dir, []string{"rc.txt"})
	w.interval = 50 * time.Millisecond
	w.Start()
	defer w.Stop()

	// Wait for snapshot.
	time.Sleep(20 * time.Millisecond)

	// Modify the file.
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, os.WriteFile(f, []byte("v2"), 0644))

	// Wait for detection.
	select {
	case <-w.Events():
		// drained
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for change event")
	}

	changes := w.RecentChanges()
	assert.Contains(t, changes, "rc.txt")

	// Second call should return empty (cleared).
	changes2 := w.RecentChanges()
	assert.Empty(t, changes2, "RecentChanges should be empty after first read")
}

func TestWatcher_MultipleWatchedPaths(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.txt")
	f2 := filepath.Join(dir, "b.txt")
	require.NoError(t, os.WriteFile(f1, []byte("a"), 0644))
	require.NoError(t, os.WriteFile(f2, []byte("b"), 0644))

	w := New(dir, []string{"a.txt", "b.txt"})
	w.interval = 50 * time.Millisecond
	w.Start()
	defer w.Stop()

	// Wait for snapshot.
	time.Sleep(20 * time.Millisecond)

	// Modify both files.
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, os.WriteFile(f1, []byte("a2"), 0644))
	require.NoError(t, os.WriteFile(f2, []byte("b2"), 0644))

	// Collect events - should get at least 2 within a few poll cycles.
	seen := map[string]bool{}
	timeout := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case ev := <-w.Events():
			seen[ev.Path] = true
		case <-timeout:
			t.Fatalf("timed out waiting for events, only saw: %v", seen)
		}
	}

	assert.True(t, seen["a.txt"], "should detect change in a.txt")
	assert.True(t, seen["b.txt"], "should detect change in b.txt")
}

func TestWatcher_DefaultFiles(t *testing.T) {
	dir := t.TempDir()
	w := New(dir, nil)
	assert.Equal(t, DefaultWatchFiles, w.files, "nil files should use defaults")
}

func TestWatcher_DetectsFileDeletion(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "ephemeral.txt")
	require.NoError(t, os.WriteFile(f, []byte("exists"), 0644))

	w := New(dir, []string{"ephemeral.txt"})
	w.interval = 50 * time.Millisecond
	w.Start()
	defer w.Stop()

	// Wait for snapshot.
	time.Sleep(20 * time.Millisecond)

	// Delete the file.
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, os.Remove(f))

	// Should get a deletion event.
	select {
	case ev := <-w.Events():
		assert.Equal(t, "ephemeral.txt", ev.Path)
		// Recent changes should note the deletion.
		changes := w.RecentChanges()
		assert.Contains(t, changes, "ephemeral.txt (deleted)")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for deletion event")
	}
}

func TestWatcher_NonexistentFileNoEvent(t *testing.T) {
	dir := t.TempDir()
	// File doesn't exist - should not produce events.
	w := New(dir, []string{"doesnotexist.txt"})
	w.interval = 50 * time.Millisecond
	w.Start()
	defer w.Stop()

	time.Sleep(150 * time.Millisecond)

	select {
	case ev := <-w.Events():
		t.Fatalf("unexpected event for nonexistent file: %v", ev)
	default:
		// expected
	}
}
