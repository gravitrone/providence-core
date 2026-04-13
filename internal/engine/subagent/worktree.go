package subagent

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// slugRe strips non-alphanumeric characters for branch-safe naming.
var slugRe = regexp.MustCompile(`[^a-zA-Z0-9-]`)

// Slugify converts a name into a branch-safe slug.
func Slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "-")
	s = slugRe.ReplaceAllString(s, "")
	if len(s) > 50 {
		s = s[:50]
	}
	if s == "" {
		s = "agent"
	}
	return s
}

// CreateWorktree creates a git worktree for agent isolation.
// The worktree is created at "../providence-agent-{slug}" relative to repoRoot,
// on a new branch "providence-agent-{slug}" from HEAD.
func CreateWorktree(repoRoot, slug string) (worktreePath, branch string, err error) {
	branch = "providence-agent-" + slug
	worktreePath = filepath.Join(filepath.Dir(repoRoot), branch)

	cmd := exec.Command("git", "worktree", "add", worktreePath, "-b", branch, "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return worktreePath, branch, nil
}

// HasWorktreeChanges checks if a worktree has uncommitted changes.
func HasWorktreeChanges(path string) bool {
	cmd := exec.Command("git", "-C", path, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

// RemoveWorktree removes a git worktree and deletes its branch.
func RemoveWorktree(repoRoot, path, branch string) error {
	// Remove worktree.
	cmd := exec.Command("git", "worktree", "remove", path)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// Delete the branch.
	cmd = exec.Command("git", "branch", "-D", branch)
	cmd.Dir = repoRoot
	out, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git branch -D: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// BuildWorktreeNotice returns prompt text telling the agent about its worktree.
func BuildWorktreeNotice(originalCWD, worktreePath string) string {
	return fmt.Sprintf(`You are working in an isolated git worktree at %s.
This is a separate copy of the repository branched from the main working directory at %s.
All your file changes are isolated here. When you are done:
- Commit your changes in the worktree.
- The coordinator will review and merge your branch.
- Do NOT push to remote - the coordinator handles that.
- Translate any paths from the original cwd (%s) to your worktree (%s).`,
		worktreePath, originalCWD, originalCWD, worktreePath)
}
