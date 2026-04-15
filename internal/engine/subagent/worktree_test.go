package subagent

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Fix Auth Bug", "fix-auth-bug"},
		{"my agent", "my-agent"},
		{"!!special!!chars!!", "specialchars"},
		{"", "agent"},
		{"   spaces   ", "spaces"},
		{"UPPER-CASE", "upper-case"},
		{"a-very-long-name-that-should-be-truncated-because-it-exceeds-fifty-characters-total", "a-very-long-name-that-should-be-truncated-because-"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := Slugify(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildWorktreeNotice(t *testing.T) {
	notice := BuildWorktreeNotice("/repo/main", "/repo/providence-agent-fix")
	assert.Contains(t, notice, "/repo/providence-agent-fix")
	assert.Contains(t, notice, "/repo/main")
	assert.Contains(t, notice, "isolated git worktree")
	assert.Contains(t, notice, "Translate any paths")
}

// initTestRepo creates a temporary git repo with one commit for worktree tests.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "cmd %v: %s", args, string(out))
	}

	// Create a file and commit so HEAD exists.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test"), 0644))
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	cmd = exec.Command("git", "commit", "-m", "init")
	cmd.Dir = dir
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	return dir
}

func TestCreateAndRemoveWorktree(t *testing.T) {
	repo := initTestRepo(t)

	path, branch, err := CreateWorktree(repo, "test-agent")
	require.NoError(t, err)
	assert.Equal(t, "providence-agent-test-agent", branch)
	assert.Contains(t, path, "providence-agent-test-agent")

	// Worktree directory should exist.
	_, err = os.Stat(path)
	require.NoError(t, err, "worktree directory should exist")

	// No changes initially.
	assert.False(t, HasWorktreeChanges(path))

	// Create a file to make changes.
	require.NoError(t, os.WriteFile(filepath.Join(path, "new.txt"), []byte("new"), 0644))
	assert.True(t, HasWorktreeChanges(path))

	// Clean up for removal (discard changes).
	cmd := exec.Command("git", "-C", path, "checkout", ".")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	os.Remove(filepath.Join(path, "new.txt"))

	// Remove worktree.
	err = RemoveWorktree(repo, path, branch)
	require.NoError(t, err)

	// Directory should be gone.
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestCreateWorktreeBadRepo(t *testing.T) {
	_, _, err := CreateWorktree("/nonexistent/repo", "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "git worktree add")
}

func TestHasWorktreeChangesBadPath(t *testing.T) {
	// Should return false for nonexistent path (not panic).
	assert.False(t, HasWorktreeChanges("/nonexistent/path"))
}

// TestSlugifyAllSpecialCharsFallsBackToAgent pins the fallback path
// when an input strips to the empty string. The Slugify contract is
// that a non-empty branch-safe slug always comes back, so downstream
// worktree/branch naming never hits an invalid git ref.
func TestSlugifyAllSpecialCharsFallsBackToAgent(t *testing.T) {
	t.Parallel()

	cases := []string{"!!!", "@@@@@", "$$$$", "   ", "\t\n", "?!?"}
	for _, input := range cases {
		assert.Equal(t, "agent", Slugify(input),
			"Slugify(%q) must fall back to %q for a valid branch name", input, "agent")
	}
}

// TestSlugifyUnicodeStripped verifies non-ASCII characters are stripped
// so branch names remain portable across filesystems and git hosts.
// Mixed ASCII + Unicode input keeps the ASCII portion.
func TestSlugifyUnicodeStripped(t *testing.T) {
	t.Parallel()

	// "héllo" - the é is a 2-byte UTF-8 sequence; regex strips non-ASCII
	// bytes individually, so the "é" disappears entirely and the
	// surrounding ASCII letters concatenate.
	assert.Equal(t, "hllo", Slugify("héllo"), "accented characters stripped byte-wise")
	assert.Equal(t, "emoji", Slugify("emoji🔥"), "emoji stripped")
	assert.Equal(t, "agent", Slugify("🔥🔥🔥"), "all-unicode falls back to agent")
}
