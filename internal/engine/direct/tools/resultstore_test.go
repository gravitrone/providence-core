package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redirectSpillDir points the spill writer at a fresh temp directory
// for the life of the calling test and restores the original on
// completion. Returns the redirected directory.
func redirectSpillDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	spillDirMu.RLock()
	prev := spillDir
	spillDirMu.RUnlock()
	SetSpillDir(dir)
	t.Cleanup(func() { SetSpillDir(prev) })
	return dir
}

// TestSpillIfLargeBelowThresholdReturnsInput verifies the fast path:
// content at or below SpillThreshold is returned unchanged and no
// file is written.
func TestSpillIfLargeBelowThresholdReturnsInput(t *testing.T) {
	dir := redirectSpillDir(t)

	body := strings.Repeat("x", SpillThreshold)
	short, path := SpillIfLarge("sess-1", "Bash", body)
	assert.Equal(t, body, short)
	assert.Empty(t, path, "content at threshold must not spill")

	entries, _ := os.ReadDir(dir)
	assert.Empty(t, entries, "no spill file must be created below threshold")
}

// TestSpillIfLargeAboveThresholdSpillsAndPreviews verifies oversized
// content writes a full-body file and returns a head + marker + tail
// preview pointing at the spill path.
func TestSpillIfLargeAboveThresholdSpillsAndPreviews(t *testing.T) {
	dir := redirectSpillDir(t)

	// Build content well past threshold with distinguishable head and
	// tail markers so the preview assertions are unambiguous.
	head := strings.Repeat("H", SpillPreviewBytes)
	mid := strings.Repeat("M", SpillThreshold)
	tail := strings.Repeat("T", SpillPreviewBytes)
	body := head + mid + tail

	short, path := SpillIfLarge("sess-2", "Bash", body)
	require.NotEmpty(t, path, "oversized content must spill")
	assert.True(t, strings.HasPrefix(path, dir+string(os.PathSeparator)),
		"spill path must live inside the redirected dir: %s", path)

	// Preview keeps the first and last SpillPreviewBytes.
	assert.Contains(t, short, head)
	assert.Contains(t, short, tail)
	assert.Contains(t, short, path, "preview must cite the spill path so the model can reference it")
	assert.Contains(t, short, "bytes elided")
	assert.Less(t, len(short), len(body), "preview must be shorter than the full body")

	// Full content landed on disk.
	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, body, string(onDisk))
}

// TestSpillIfLargeSanitisesPathComponents verifies weird session or
// tool names cannot inject path separators via the spill filename.
func TestSpillIfLargeSanitisesPathComponents(t *testing.T) {
	dir := redirectSpillDir(t)

	body := strings.Repeat("x", SpillThreshold+1)
	_, path := SpillIfLarge("../evil", "../Escape", body)
	require.NotEmpty(t, path)
	// Path lives under the redirected dir, not jumped elsewhere.
	abs, _ := filepath.Abs(path)
	root, _ := filepath.Abs(dir)
	assert.True(t, strings.HasPrefix(abs, root),
		"sanitised session must not escape the spill root: got %s root %s", abs, root)
}

// TestSpillIfLargeEmptySessionUsesFallback verifies the default
// namespace when a caller passes an empty session ID.
func TestSpillIfLargeEmptySessionUsesFallback(t *testing.T) {
	dir := redirectSpillDir(t)

	body := strings.Repeat("y", SpillThreshold+1)
	_, path := SpillIfLarge("", "Bash", body)
	require.NotEmpty(t, path)
	assert.Contains(t, path, filepath.Join(dir, "session"),
		"empty session id must fall back to the 'session' directory")
}
