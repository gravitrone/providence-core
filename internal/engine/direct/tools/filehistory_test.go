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

// redirectFileHistoryDir points the history writer at a fresh temp
// directory for the life of the calling test.
func redirectFileHistoryDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	fileHistoryDirMu.RLock()
	prev := fileHistoryDir
	fileHistoryDirMu.RUnlock()
	SetFileHistoryDir(dir)
	t.Cleanup(func() { SetFileHistoryDir(prev) })
	return dir
}

// TestSnapshotFileMissingSourceIsNoOp verifies Snapshot on a
// non-existent path returns (empty, nil) so the Edit/Write first-use
// path does not error out.
func TestSnapshotFileMissingSourceIsNoOp(t *testing.T) {
	redirectFileHistoryDir(t)

	snap, err := SnapshotFile(filepath.Join(t.TempDir(), "missing.txt"))
	require.NoError(t, err)
	assert.Empty(t, snap.ID)
	assert.Empty(t, snap.Path)
}

// TestSnapshotRoundTripRestoresOriginal verifies the basic contract:
// Snapshot existing content, mutate, then Restore via the snapshot id
// brings the file back byte-for-byte.
func TestSnapshotRoundTripRestoresOriginal(t *testing.T) {
	redirectFileHistoryDir(t)

	tmp := t.TempDir()
	path := filepath.Join(tmp, "doc.md")
	original := []byte("# Original\n\ncontents\n")
	require.NoError(t, os.WriteFile(path, original, 0o644))

	snap, err := SnapshotFile(path)
	require.NoError(t, err)
	require.NotEmpty(t, snap.ID)
	assert.Equal(t, path, snap.Path)
	assert.Equal(t, int64(len(original)), snap.Bytes)

	// Mutate to bad content.
	require.NoError(t, os.WriteFile(path, []byte("WRECKED"), 0o644))

	// Restore.
	require.NoError(t, RestoreSnapshot(path, snap.ID))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original, got)
}

// TestListSnapshotsOrdersNewestFirst verifies the list API returns
// snapshots sorted by creation time descending so the most-recent
// entry is always first.
func TestListSnapshotsOrdersNewestFirst(t *testing.T) {
	redirectFileHistoryDir(t)

	tmp := t.TempDir()
	path := filepath.Join(tmp, "doc.md")
	require.NoError(t, os.WriteFile(path, []byte("v1"), 0o644))

	s1, err := SnapshotFile(path)
	require.NoError(t, err)
	// Sleep a millisecond so the second snapshot has a later unix-ms
	// id and a later modtime.
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, os.WriteFile(path, []byte("v2"), 0o644))
	s2, err := SnapshotFile(path)
	require.NoError(t, err)

	snaps, err := ListSnapshots(path)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(snaps), 2)
	assert.Equal(t, s2.ID, snaps[0].ID, "newest snapshot must come first")
	assert.Equal(t, s1.ID, snaps[1].ID)
}

// TestListSnapshotsUnknownPathReturnsEmpty verifies a path with no
// history returns nil, no error.
func TestListSnapshotsUnknownPathReturnsEmpty(t *testing.T) {
	redirectFileHistoryDir(t)

	snaps, err := ListSnapshots("/nowhere/ever.txt")
	require.NoError(t, err)
	assert.Empty(t, snaps)
}

// TestRestoreSnapshotUnknownIDReturnsError verifies the failure
// contract for a bad snapshot id.
func TestRestoreSnapshotUnknownIDReturnsError(t *testing.T) {
	redirectFileHistoryDir(t)

	err := RestoreSnapshot("/tmp/x", "not-a-real-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open snapshot")
}

// TestFileHistoryToolListEmptyReturnsMessage verifies the list
// operation surfaces a clear "no snapshots" message instead of an
// empty result.
func TestFileHistoryToolListEmptyReturnsMessage(t *testing.T) {
	redirectFileHistoryDir(t)

	tool := NewFileHistoryTool()
	res := tool.Execute(context.Background(), map[string]any{
		"operation": "list",
		"file_path": "/tmp/never-snapshotted.txt",
	})
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content, "no snapshots")
}

// TestFileHistoryToolListAfterSnapshotPrintsIDs verifies the list
// operation returns the snapshot id, timestamp, and byte count in a
// scannable format.
func TestFileHistoryToolListAfterSnapshotPrintsIDs(t *testing.T) {
	redirectFileHistoryDir(t)

	tmp := t.TempDir()
	path := filepath.Join(tmp, "list.txt")
	require.NoError(t, os.WriteFile(path, []byte("v1"), 0o644))
	snap, err := SnapshotFile(path)
	require.NoError(t, err)

	tool := NewFileHistoryTool()
	res := tool.Execute(context.Background(), map[string]any{
		"operation": "list",
		"file_path": path,
	})
	require.False(t, res.IsError)
	assert.Contains(t, res.Content, snap.ID)
	assert.Contains(t, res.Content, "bytes")
	assert.True(t, strings.HasPrefix(res.Content, "Snapshots for "))
}

// TestFileHistoryToolRestoreRequiresSnapshotID verifies the restore
// operation refuses without the snapshot_id parameter.
func TestFileHistoryToolRestoreRequiresSnapshotID(t *testing.T) {
	tool := NewFileHistoryTool()
	res := tool.Execute(context.Background(), map[string]any{
		"operation": "restore",
		"file_path": "/tmp/x",
	})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "snapshot_id is required")
}

// TestFileHistoryToolUnknownOperationRejected verifies operations
// outside list/restore fail loudly.
func TestFileHistoryToolUnknownOperationRejected(t *testing.T) {
	tool := NewFileHistoryTool()
	res := tool.Execute(context.Background(), map[string]any{
		"operation": "wipe",
		"file_path": "/tmp/x",
	})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "unknown operation")
}

// TestEvictOldSnapshotsRemovesOldest verifies the retention policy
// caps a path at historyMaxPerPath entries by deleting the oldest
// snapshots on subsequent writes.
func TestEvictOldSnapshotsRemovesOldest(t *testing.T) {
	redirectFileHistoryDir(t)

	tmp := t.TempDir()
	path := filepath.Join(tmp, "capped.txt")

	// Write and snapshot one more than the cap so eviction fires.
	for i := 0; i <= historyMaxPerPath; i++ {
		require.NoError(t, os.WriteFile(path, []byte{byte('a' + i%10)}, 0o644))
		_, err := SnapshotFile(path)
		require.NoError(t, err)
		time.Sleep(2 * time.Millisecond) // ensure distinct unix-ms ids
	}

	snaps, err := ListSnapshots(path)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(snaps), historyMaxPerPath,
		"retention must cap snapshot count per path")
}

// TestSnapshotFileEvictsExpiredSnapshotsOnNextWrite verifies age-based
// retention removes expired entries when the next snapshot is taken.
func TestSnapshotFileEvictsExpiredSnapshotsOnNextWrite(t *testing.T) {
	redirectFileHistoryDir(t)

	tmp := t.TempDir()
	path := filepath.Join(tmp, "aged.txt")

	var ids []string
	for i := 0; i < 3; i++ {
		require.NoError(t, os.WriteFile(path, []byte{byte('a' + i)}, 0o644))
		snap, err := SnapshotFile(path)
		require.NoError(t, err)
		ids = append(ids, snap.ID)
		time.Sleep(2 * time.Millisecond)
	}

	historyDir := filepath.Join(defaultFileHistoryDir(), pathKey(path))
	expiredAt := time.Now().Add(-historyMaxAge - time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(historyDir, ids[0]+".gz"), expiredAt, expiredAt))

	require.NoError(t, os.WriteFile(path, []byte("fresh"), 0o644))
	fresh, err := SnapshotFile(path)
	require.NoError(t, err)

	snaps, err := ListSnapshots(path)
	require.NoError(t, err)

	gotIDs := make([]string, 0, len(snaps))
	for _, snap := range snaps {
		gotIDs = append(gotIDs, snap.ID)
	}

	assert.NotContains(t, gotIDs, ids[0], "expired snapshot must be evicted on next write")
	assert.Contains(t, gotIDs, ids[1])
	assert.Contains(t, gotIDs, ids[2])
	assert.Contains(t, gotIDs, fresh.ID)
}

// TestSnapshotFileEvictsOldestSnapshotWhenCountExceeded verifies count-based
// retention drops the oldest snapshot once the per-path cap is exceeded.
func TestSnapshotFileEvictsOldestSnapshotWhenCountExceeded(t *testing.T) {
	redirectFileHistoryDir(t)

	tmp := t.TempDir()
	path := filepath.Join(tmp, "count.txt")

	ids := make([]string, 0, historyMaxPerPath+1)
	for i := 0; i <= historyMaxPerPath; i++ {
		require.NoError(t, os.WriteFile(path, []byte{byte('a' + i%10)}, 0o644))
		snap, err := SnapshotFile(path)
		require.NoError(t, err)
		ids = append(ids, snap.ID)
		time.Sleep(2 * time.Millisecond)
	}

	snaps, err := ListSnapshots(path)
	require.NoError(t, err)
	require.Len(t, snaps, historyMaxPerPath)

	gotIDs := make([]string, 0, len(snaps))
	for _, snap := range snaps {
		gotIDs = append(gotIDs, snap.ID)
	}

	assert.NotContains(t, gotIDs, ids[0], "oldest snapshot must be evicted when count exceeds cap")
	assert.Contains(t, gotIDs, ids[len(ids)-1], "newest snapshot must be retained")
}
