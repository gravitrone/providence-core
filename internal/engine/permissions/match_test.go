package permissions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchExact(t *testing.T) {
	assert.True(t, matchPattern("Bash", "Bash", nil))
	assert.False(t, matchPattern("Bash", "Read", nil))
}

func TestMatchWithGlob(t *testing.T) {
	assert.True(t, matchPattern("Bash(git *)", "Bash", bashInput("git push")))
	assert.True(t, matchPattern("Bash(git *)", "Bash", bashInput("git status")))
}

func TestMatchWithPath(t *testing.T) {
	assert.True(t, matchPattern("Read(/home/*)", "Read", fileInput("/home/user")))
	assert.True(t, matchPattern("Write(/tmp/*)", "Write", fileInput("/tmp/out.txt")))
}

func TestMatchNoMatch(t *testing.T) {
	assert.False(t, matchPattern("Bash(npm *)", "Bash", bashInput("git push")))
	assert.False(t, matchPattern("Read(/etc/*)", "Read", fileInput("/home/user")))
}

func TestIsSafetyPathGit(t *testing.T) {
	assert.True(t, isSafetyPath("Bash", bashInput("cat .git/hooks/pre-commit")))
	assert.True(t, isSafetyPath("Write", fileInput("/repo/.git/config")))
}

func TestIsSafetyPathClaude(t *testing.T) {
	assert.True(t, isSafetyPath("Write", fileInput("/project/.claude/settings.json")))
	assert.True(t, isSafetyPath("Edit", fileInput("/home/user/.claude/config.toml")))
}

func TestNormalPathNotSafety(t *testing.T) {
	assert.False(t, isSafetyPath("Read", fileInput("internal/main.go")))
	assert.False(t, isSafetyPath("Bash", bashInput("go build ./...")))
	assert.False(t, isSafetyPath("Write", fileInput("/tmp/output.txt")))
}
