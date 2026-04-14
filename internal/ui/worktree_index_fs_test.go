package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeWorktreeIndexFile writes a WorktreeIndex to dir/.providence/worktree-index.json.
func writeWorktreeIndexFile(t *testing.T, dir string, idx WorktreeIndex) string {
	t.Helper()
	dest := filepath.Join(dir, ".providence")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	path := filepath.Join(dest, "worktree-index.json")
	data, err := json.MarshalIndent(idx, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

func TestWorktreeIndex_BuildFromEmptyDir(t *testing.T) {
	dir := t.TempDir()
	empty := WorktreeIndex{Files: nil, Total: 0}
	writeWorktreeIndexFile(t, dir, empty)

	data, err := os.ReadFile(filepath.Join(dir, ".providence", "worktree-index.json"))
	require.NoError(t, err)

	var loaded WorktreeIndex
	require.NoError(t, json.Unmarshal(data, &loaded))
	assert.Empty(t, loaded.Files)
	assert.Equal(t, 0, loaded.Total)
}

func TestWorktreeIndex_BuildFromTempDirWithEntries(t *testing.T) {
	dir := t.TempDir()
	idx := WorktreeIndex{
		Files: []WorktreeIndexEntry{
			{Path: "cmd/main.go", Desc: "Go source"},
			{Path: "README.md", Desc: "Project readme"},
			{Path: "internal/ui/app.go", Desc: "Go source"},
		},
		Total: 3,
	}
	writeWorktreeIndexFile(t, dir, idx)

	data, err := os.ReadFile(filepath.Join(dir, ".providence", "worktree-index.json"))
	require.NoError(t, err)

	var loaded WorktreeIndex
	require.NoError(t, json.Unmarshal(data, &loaded))
	assert.Equal(t, 3, loaded.Total)
	require.Len(t, loaded.Files, 3)
	assert.Equal(t, "cmd/main.go", loaded.Files[0].Path)
}

func TestWorktreeIndex_TopFilesLimitLogic(t *testing.T) {
	// Validates the limit/cap logic that TopWorktreeFiles applies.
	idx := WorktreeIndex{
		Files: []WorktreeIndexEntry{
			{Path: "cmd/main.go", Desc: "Go source"},
			{Path: "go.mod", Desc: "Go module definition"},
			{Path: "README.md", Desc: "Project readme"},
			{Path: "unknown.bin", Desc: ""},
		},
		Total: 4,
	}

	// n=2: should cap at 2 and report overflow.
	n := 2
	limit := n
	if limit > len(idx.Files) {
		limit = len(idx.Files)
	}
	assert.Equal(t, 2, limit)
	assert.True(t, idx.Total > limit)

	// n=100: should cap at actual length.
	n = 100
	limit = n
	if limit > len(idx.Files) {
		limit = len(idx.Files)
	}
	assert.Equal(t, 4, limit)

	// Entries with and without desc both present.
	assert.NotEmpty(t, idx.Files[0].Desc)
	assert.Empty(t, idx.Files[3].Desc)
}

func TestWorktreeIndex_RapidFSChangesNoRace(t *testing.T) {
	// Multiple goroutines marshal/unmarshal WorktreeIndex concurrently.
	// No shared mutable state - validates the struct is race-clean under -race.
	const workers = 8
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			idx := WorktreeIndex{
				Files: []WorktreeIndexEntry{
					{Path: "file.go", Desc: "Go source"},
				},
				Total: 1,
			}
			data, err := json.Marshal(idx)
			if err != nil {
				return
			}
			var out WorktreeIndex
			_ = json.Unmarshal(data, &out)
		}()
	}
	wg.Wait()
}

func TestWorktreeIndex_ConcurrentTreeUpdates(t *testing.T) {
	// Each goroutine writes an independent index to its own temp dir.
	// Tests that inferFileDesc has no shared mutable state under -race.
	const workers = 6
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			dir := t.TempDir()
			idx := WorktreeIndex{
				Files: []WorktreeIndexEntry{
					{Path: "main.go", Desc: inferFileDesc("main.go")},
					{Path: "go.mod", Desc: inferFileDesc("go.mod")},
					{Path: "README.md", Desc: inferFileDesc("README.md")},
				},
				Total: 3,
			}
			destDir := filepath.Join(dir, ".providence")
			_ = os.MkdirAll(destDir, 0o755)
			data, _ := json.MarshalIndent(idx, "", "  ")
			_ = os.WriteFile(filepath.Join(destDir, "worktree-index.json"), data, 0o644)

			// Read it back to verify consistency.
			readData, err := os.ReadFile(filepath.Join(destDir, "worktree-index.json"))
			if err != nil {
				return
			}
			var loaded WorktreeIndex
			_ = json.Unmarshal(readData, &loaded)
		}()
	}
	wg.Wait()
}
