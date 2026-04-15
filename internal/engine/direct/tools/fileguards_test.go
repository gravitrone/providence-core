package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSizeGuardErrorMissingFileAllowsCreate verifies the guard returns
// empty for a non-existent path so Write-as-create stays unblocked.
func TestSizeGuardErrorMissingFileAllowsCreate(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "not-yet.txt")
	assert.Empty(t, SizeGuardError(missing))
}

// TestSizeGuardErrorSmallFileAllowed verifies normal-size files pass.
func TestSizeGuardErrorSmallFileAllowed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.txt")
	require.NoError(t, os.WriteFile(path, []byte("tiny"), 0o644))
	assert.Empty(t, SizeGuardError(path))
}

// TestSizeGuardErrorOverLimitRejected verifies the 100 MiB cap fires
// by using a sparse file (no disk cost) that Stat reports as large.
func TestSizeGuardErrorOverLimitRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "big.bin")

	// Sparse file: Seek past 100 MiB and write one byte so Size() is
	// huge without allocating real disk.
	f, err := os.Create(path)
	require.NoError(t, err)
	_, err = f.Seek(MaxEditableFileSize+1, 0)
	require.NoError(t, err)
	_, err = f.Write([]byte{0})
	require.NoError(t, err)
	require.NoError(t, f.Close())

	msg := SizeGuardError(path)
	assert.Contains(t, msg, "refuse targets larger than")
}

// TestIsSettingsFileMatchesProvidenceAndClaudePaths verifies the
// suffix matcher catches both absolute and relative forms of the
// protected settings files.
func TestIsSettingsFileMatchesProvidenceAndClaudePaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path string
		want bool
	}{
		{"/home/u/.providence/config.toml", true},
		{"./.providence/config.toml", true},
		{"./.providence/permissions.toml", true},
		{"/tmp/x/.claude/settings.json", true},
		{"/tmp/x/.claude/settings.local.json", true},
		{"/tmp/other.toml", false},
		{"/tmp/fake/providence-config.toml", false},
		{"", false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, IsSettingsFile(tc.path), "IsSettingsFile(%q)", tc.path)
	}
}

// TestValidateSettingsContentTOMLRejectsBrokenInput pins the validator
// on a malformed providence config. An unclosed bracket is a common
// accident pattern for an assistant rewriting the file from scratch.
func TestValidateSettingsContentTOMLRejectsBrokenInput(t *testing.T) {
	t.Parallel()

	err := ValidateSettingsContent("/home/u/.providence/config.toml", `[unclosed`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TOML")
}

// TestValidateSettingsContentTOMLAcceptsValid verifies a real config
// body parses and returns nil.
func TestValidateSettingsContentTOMLAcceptsValid(t *testing.T) {
	t.Parallel()

	body := `engine = "direct"
model = "claude-sonnet-4-6"
persona = "bro"
`
	assert.NoError(t, ValidateSettingsContent("/home/u/.providence/config.toml", body))
}

// TestValidateSettingsContentJSONRejectsBrokenInput covers the
// .claude/settings.json validator branch.
func TestValidateSettingsContentJSONRejectsBrokenInput(t *testing.T) {
	t.Parallel()

	err := ValidateSettingsContent("/tmp/x/.claude/settings.json", `{"broken":`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JSON")
}

// TestValidateSettingsContentNonSettingsFileIsNoOp verifies paths
// outside the settings-file set skip validation entirely.
func TestValidateSettingsContentNonSettingsFileIsNoOp(t *testing.T) {
	t.Parallel()
	// This is not a settings path, so garbage content is accepted.
	assert.NoError(t, ValidateSettingsContent("/tmp/random.txt", `definitely not toml { broken`))
}
