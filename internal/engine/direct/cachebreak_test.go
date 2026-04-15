package direct

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gravitrone/providence-core/internal/engine"
)

// redirectCacheBreakDir points the cache-break writer at a fresh temp
// directory for the life of the calling test. Returns the directory.
// t.Cleanup restores the original dir on test completion.
func redirectCacheBreakDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Save the current value (private) by reading via the getter and
	// restoring via the setter. The real restoration happens on test
	// completion.
	cacheBreakDirMu.RLock()
	prev := cacheBreakDir
	cacheBreakDirMu.RUnlock()
	SetCacheBreakDir(dir)
	t.Cleanup(func() { SetCacheBreakDir(prev) })
	return dir
}

// TestFingerprintFromInputsHashesPerBlockAndTool verifies every block
// text and tool schema lands as its own hash entry. Shared fingerprint
// layout with the diff function below.
func TestFingerprintFromInputsHashesPerBlockAndTool(t *testing.T) {
	t.Parallel()

	blocks := []engine.SystemBlock{
		{Text: "identity block"},
		{Text: "coding guidelines"},
	}
	fp := fingerprintFromInputs("claude-sonnet-4-6", blocks, nil)

	assert.Equal(t, "claude-sonnet-4-6", fp.Model)
	require.Len(t, fp.SystemHashes, 2)
	assert.NotEmpty(t, fp.SystemHashes[0])
	assert.NotEqual(t, fp.SystemHashes[0], fp.SystemHashes[1],
		"distinct block texts must produce distinct hashes")
	assert.Empty(t, fp.ToolHashes, "nil registry yields empty tool hash map")
}

// TestDiffFingerprintsFirstCallReturnsNilDiff verifies the empty-prev
// case: the first fingerprint recorded in a session has nothing to
// diff against, so the function must suppress output.
func TestDiffFingerprintsFirstCallReturnsNilDiff(t *testing.T) {
	t.Parallel()

	next := fingerprintFromInputs("m", []engine.SystemBlock{{Text: "a"}}, nil)
	diff := DiffCacheFingerprints(CacheFingerprint{}, next)
	assert.Nil(t, diff, "first call must not produce a spurious diff")
}

// TestDiffFingerprintsIdenticalReturnsEmpty verifies the no-drift case:
// two identical fingerprints produce an empty diff so no file is
// written on a stable turn-to-turn cache.
func TestDiffFingerprintsIdenticalReturnsEmpty(t *testing.T) {
	t.Parallel()

	fp := fingerprintFromInputs("m", []engine.SystemBlock{{Text: "a"}, {Text: "b"}}, nil)
	diff := DiffCacheFingerprints(fp, fp)
	assert.Empty(t, diff, "identical fingerprints must produce no diff lines")
}

// TestDiffFingerprintsBlockTextChangeNamesIndex verifies that a single
// block-text change surfaces as a one-line diff naming the block
// index and both old + new hashes so the operator can identify which
// block flipped.
func TestDiffFingerprintsBlockTextChangeNamesIndex(t *testing.T) {
	t.Parallel()

	prev := fingerprintFromInputs("m", []engine.SystemBlock{
		{Text: "identity"},
		{Text: "coding"},
	}, nil)
	next := fingerprintFromInputs("m", []engine.SystemBlock{
		{Text: "identity"},
		{Text: "coding v2"}, // index 1 flipped
	}, nil)

	diff := DiffCacheFingerprints(prev, next)
	require.Len(t, diff, 1)
	assert.Contains(t, diff[0], "system_block[1]")
	assert.Contains(t, diff[0], " -> ")
}

// TestDiffFingerprintsModelChangeNamed verifies a model swap surfaces
// as its own line separate from any block diffs.
func TestDiffFingerprintsModelChangeNamed(t *testing.T) {
	t.Parallel()

	prev := fingerprintFromInputs("old", []engine.SystemBlock{{Text: "x"}}, nil)
	next := fingerprintFromInputs("new", []engine.SystemBlock{{Text: "x"}}, nil)
	diff := DiffCacheFingerprints(prev, next)
	require.Len(t, diff, 1)
	assert.Contains(t, diff[0], "model:")
	assert.Contains(t, diff[0], `"old"`)
	assert.Contains(t, diff[0], `"new"`)
}

// TestDiffFingerprintsBlockAddedAndRemoved verifies index-based layout
// handles growth and shrinkage without mis-labeling.
func TestDiffFingerprintsBlockAddedAndRemoved(t *testing.T) {
	t.Parallel()

	prev := fingerprintFromInputs("m", []engine.SystemBlock{
		{Text: "a"}, {Text: "b"}, {Text: "c"},
	}, nil)
	next := fingerprintFromInputs("m", []engine.SystemBlock{
		{Text: "a"}, {Text: "b"},
	}, nil)
	diff := DiffCacheFingerprints(prev, next)
	require.Len(t, diff, 1)
	assert.Contains(t, diff[0], "system_block[2]: removed")

	// Reverse direction.
	diffBack := DiffCacheFingerprints(next, prev)
	require.Len(t, diffBack, 1)
	assert.Contains(t, diffBack[0], "system_block[2]: added")
}

// TestWriteCacheBreakDiffCreatesFileWithExpectedBody verifies the
// writer creates the directory, names the file with the timestamp +
// 6-char tag, and writes newline-terminated body.
func TestWriteCacheBreakDiffCreatesFileWithExpectedBody(t *testing.T) {
	dir := redirectCacheBreakDir(t)

	lines := []string{"model: \"a\" -> \"b\"", "system_block[0]: abc -> def"}
	fixedTime := time.Date(2026, 4, 16, 15, 30, 45, 0, time.UTC)

	path, err := WriteCacheBreakDiff(lines, fixedTime)
	require.NoError(t, err)
	require.NotEmpty(t, path)
	assert.True(t, strings.HasPrefix(filepath.Base(path), "20260416-153045-"),
		"filename must start with the fixed timestamp: %s", path)
	assert.True(t, strings.HasSuffix(path, ".diff"))
	assert.Equal(t, dir, filepath.Dir(path), "file must land in the redirected dir")

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(body)
	assert.Contains(t, text, lines[0])
	assert.Contains(t, text, lines[1])
	assert.True(t, strings.HasSuffix(text, "\n"), "body must end with newline")
}

// TestWriteCacheBreakDiffEmptyLinesSkipsFile verifies we do not create
// empty diff files on stable turns.
func TestWriteCacheBreakDiffEmptyLinesSkipsFile(t *testing.T) {
	dir := redirectCacheBreakDir(t)

	path, err := WriteCacheBreakDiff(nil, time.Now())
	require.NoError(t, err)
	assert.Empty(t, path, "empty diff must not write a file")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "redirect dir must remain empty on a no-op write")
}

// TestHash64IsStable verifies the hash is deterministic across runs
// so diff files remain idempotent when the inputs are the same.
func TestHash64IsStable(t *testing.T) {
	t.Parallel()

	a := hash64("providence system block identity")
	b := hash64("providence system block identity")
	assert.Equal(t, a, b)
	assert.Len(t, a, 16, "fnv64 hex must be exactly 16 chars")
}
