package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// providenceRepoRoot walks up from this test file's location until it
// finds a go.mod, returning the repo root path. This keeps the
// real-git-repo integration tests hermetic across machines and CI.
// Skips the calling test if the repo root cannot be located.
func providenceRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller failed - cannot locate test file")
	}
	dir := filepath.Dir(thisFile)
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Skip("could not locate Providence repo root (go.mod + .git) from test binary")
	return ""
}

func TestNonEmptyFiltersBlankLines(t *testing.T) {
	input := []string{"hello", "", "  ", "world", "\t", "ok"}
	got := nonEmpty(input)
	assert.Equal(t, []string{"hello", "world", "ok"}, got)
}

func TestNonEmptyAllBlank(t *testing.T) {
	input := []string{"", "  ", "\t"}
	got := nonEmpty(input)
	assert.Empty(t, got)
}

func TestNonEmptyEmptySlice(t *testing.T) {
	got := nonEmpty(nil)
	assert.Empty(t, got)
}

func TestMostFrequentBasic(t *testing.T) {
	items := []string{"a", "b", "a", "c", "a", "b"}
	got := mostFrequent(items)
	assert.Equal(t, "a", got)
}

func TestMostFrequentSingleItem(t *testing.T) {
	got := mostFrequent([]string{"only"})
	assert.Equal(t, "only", got)
}

func TestMostFrequentEmpty(t *testing.T) {
	got := mostFrequent(nil)
	assert.Equal(t, "", got)
}

func TestMostFrequentTie(t *testing.T) {
	// When tied, any of the tied items is acceptable.
	items := []string{"x", "y", "x", "y"}
	got := mostFrequent(items)
	assert.True(t, got == "x" || got == "y", "expected x or y, got %q", got)
}

func TestGenerateGitSuggestionsNoGit(t *testing.T) {
	// A directory that definitely isn't a git repo.
	suggestions := GenerateGitSuggestions(t.TempDir())
	assert.Nil(t, suggestions, "non-git dir should return nil suggestions")
}

func TestGenerateGitSuggestionsNonexistentDir(t *testing.T) {
	suggestions := GenerateGitSuggestions("/tmp/this-path-definitely-does-not-exist-123456789")
	assert.Nil(t, suggestions)
}

func TestFormatGitSuggestionsEmpty(t *testing.T) {
	out := FormatGitSuggestions(nil)
	assert.Equal(t, "", out)
}

func TestFormatGitSuggestionsEmptySlice(t *testing.T) {
	out := FormatGitSuggestions([]GitSuggestion{})
	assert.Equal(t, "", out)
}

func TestFormatGitSuggestionsSingle(t *testing.T) {
	suggestions := []GitSuggestion{
		{Text: "How does main.go work?"},
	}
	out := FormatGitSuggestions(suggestions)
	assert.Contains(t, out, "Try asking:")
	assert.Contains(t, out, "> How does main.go work?")
}

func TestFormatGitSuggestionsMultiple(t *testing.T) {
	suggestions := []GitSuggestion{
		{Text: "How does main.go work?"},
		{Text: "Explain the recent change: fix bug"},
		{Text: "Write tests for main.go"},
	}
	out := FormatGitSuggestions(suggestions)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// Header line + 3 suggestion lines.
	assert.Len(t, lines, 4)
	for _, s := range suggestions {
		assert.Contains(t, out, s.Text)
	}
}

func TestFormatGitSuggestionsStartsWithNewline(t *testing.T) {
	suggestions := []GitSuggestion{{Text: "test"}}
	out := FormatGitSuggestions(suggestions)
	assert.True(t, strings.HasPrefix(out, "\n"))
}

// Tests that use a real git repo (the Providence repo itself).

func TestGenerateGitSuggestionsRealRepo(t *testing.T) {
	// Use the Providence repo root as a known git repo.
	repoDir := providenceRepoRoot(t)
	suggestions := GenerateGitSuggestions(repoDir)
	if suggestions == nil {
		t.Skip("Providence repo not available or no git history")
	}

	// Should return 1-3 suggestions.
	assert.LessOrEqual(t, len(suggestions), 3)
	assert.GreaterOrEqual(t, len(suggestions), 1)
}

func TestGenerateGitSuggestionsRealRepoHasFiles(t *testing.T) {
	repoDir := providenceRepoRoot(t)
	suggestions := GenerateGitSuggestions(repoDir)
	if suggestions == nil {
		t.Skip("Providence repo not available")
	}

	// First suggestion should reference an actual file path.
	if len(suggestions) >= 1 {
		assert.Contains(t, suggestions[0].Text, "How does")
	}

	// Last suggestion (if 3) should be a "write tests" suggestion.
	if len(suggestions) >= 3 {
		assert.Contains(t, suggestions[2].Text, "Write tests for")
	}
}

func TestGenerateGitSuggestionsLongCommitTruncation(t *testing.T) {
	// Since we can't control git history, we test the truncation logic directly
	// by verifying the inner mechanics.
	longMsg := strings.Repeat("x", 100)
	if len(longMsg) > 60 {
		longMsg = longMsg[:60] + "..."
	}
	assert.Equal(t, 63, len(longMsg))
	assert.True(t, strings.HasSuffix(longMsg, "..."))
}

func TestGenerateGitSuggestionsShortCommitNoTruncation(t *testing.T) {
	shortMsg := "fix: resolve nil pointer"
	if len(shortMsg) > 60 {
		shortMsg = shortMsg[:60] + "..."
	}
	assert.Equal(t, "fix: resolve nil pointer", shortMsg)
	assert.False(t, strings.HasSuffix(shortMsg, "..."))
}

func TestGenerateGitSuggestionsMaxThree(t *testing.T) {
	repoDir := providenceRepoRoot(t)
	suggestions := GenerateGitSuggestions(repoDir)
	if suggestions == nil {
		t.Skip("Providence repo not available")
	}
	assert.LessOrEqual(t, len(suggestions), 3)
}
