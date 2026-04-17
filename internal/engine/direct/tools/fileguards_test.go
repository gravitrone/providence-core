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

// TestIsDangerousPathCoversCCList verifies the dangerous-path matcher
// covers the Claude Code reference list, including exact-file matches,
// directory-prefix matches, and canonicalization against bypasses.
func TestIsDangerousPathCoversCCList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.NoError(t, os.MkdirAll(filepath.Join(home, ".ssh"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(home, "Documents"), 0o755))

	sshLink := filepath.Join(home, "ssh-link")
	require.NoError(t, os.Symlink(filepath.Join(home, ".ssh"), sshLink))

	docsLink := filepath.Join(home, "docs-link")
	require.NoError(t, os.Symlink(filepath.Join(home, "Documents"), docsLink))

	cases := []struct {
		name string
		path string
		want bool
	}{
		{name: "bashrc", path: filepath.Join(home, ".bashrc"), want: true},
		{name: "bash_profile", path: filepath.Join(home, ".bash_profile"), want: true},
		{name: "bash_login", path: filepath.Join(home, ".bash_login"), want: true},
		{name: "zshrc", path: filepath.Join(home, ".zshrc"), want: true},
		{name: "zshenv", path: filepath.Join(home, ".zshenv"), want: true},
		{name: "zprofile", path: filepath.Join(home, ".zprofile"), want: true},
		{name: "zlogin", path: filepath.Join(home, ".zlogin"), want: true},
		{name: "profile", path: filepath.Join(home, ".profile"), want: true},
		{name: "fish config", path: filepath.Join(home, ".config", "fish", "config.fish"), want: true},
		{name: "nushell config", path: filepath.Join(home, ".config", "nushell", "config.nu"), want: true},
		{name: "ssh dir", path: filepath.Join(home, ".ssh"), want: true},
		{name: "ssh child", path: filepath.Join(home, ".ssh", "config"), want: true},
		{name: "aws dir", path: filepath.Join(home, ".aws"), want: true},
		{name: "aws child", path: filepath.Join(home, ".aws", "credentials"), want: true},
		{name: "gcloud dir", path: filepath.Join(home, ".gcloud"), want: true},
		{name: "gcloud child", path: filepath.Join(home, ".gcloud", "application_default_credentials.json"), want: true},
		{name: "docker config", path: filepath.Join(home, ".docker", "config.json"), want: true},
		{name: "netrc", path: filepath.Join(home, ".netrc"), want: true},
		{name: "gitconfig", path: filepath.Join(home, ".gitconfig"), want: true},
		{name: "npmrc", path: filepath.Join(home, ".npmrc"), want: true},
		{name: "pypirc", path: filepath.Join(home, ".pypirc"), want: true},
		{name: "kube config", path: filepath.Join(home, ".kube", "config"), want: true},
		{name: "chrome dir", path: filepath.Join(home, "Library", "Application Support", "Google", "Chrome"), want: true},
		{name: "chrome child", path: filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "Default", "Cookies"), want: true},
		{name: "firefox dir", path: filepath.Join(home, "Library", "Application Support", "Firefox"), want: true},
		{name: "firefox child", path: filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles", "default-release", "cookies.sqlite"), want: true},
		{name: "keychains dir", path: filepath.Join(home, "Library", "Keychains"), want: true},
		{name: "keychains child", path: filepath.Join(home, "Library", "Keychains", "login.keychain-db"), want: true},
		{name: "sudoers", path: "/etc/sudoers", want: true},
		{name: "passwd", path: "/etc/passwd", want: true},
		{name: "shadow", path: "/etc/shadow", want: true},
		{name: "etc ssh dir", path: "/etc/ssh", want: true},
		{name: "etc ssh child", path: "/etc/ssh/sshd_config", want: true},
		{name: "project file", path: filepath.Join(home, "workspace", "main.go"), want: false},
		{name: "documents file", path: filepath.Join(home, "Documents", "foo.txt"), want: false},
		{name: "similar home file", path: filepath.Join(home, ".gitconfig.backup"), want: false},
		{name: "similar etc file", path: "/etc/passwd.bak", want: false},
		{name: "clean dot dot bypass", path: filepath.Join(home, "workspace", "..", ".ssh", "config"), want: true},
		{name: "symlink into ssh", path: filepath.Join(sshLink, "config"), want: true},
		{name: "symlink into documents", path: filepath.Join(docsLink, "foo.txt"), want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsDangerousPath(tc.path))
		})
	}
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
