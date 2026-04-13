package ui

import (
	"fmt"
	"os/exec"
	"strings"
)

// GitSuggestion is a contextual first-run suggestion based on git history.
type GitSuggestion struct {
	Text string
}

// GenerateGitSuggestions scans the git history of the given directory and
// returns up to 3 contextual example prompts. Returns nil (no error) if
// git is unavailable or the directory is not a repo.
func GenerateGitSuggestions(dir string) []GitSuggestion {
	// Get recent commit messages.
	commitCmd := exec.Command("git", "log", "--format=%s", "-20")
	commitCmd.Dir = dir
	commitOut, err := commitCmd.Output()
	if err != nil {
		return nil
	}
	commits := nonEmpty(strings.Split(strings.TrimSpace(string(commitOut)), "\n"))

	// Get most-edited files.
	fileCmd := exec.Command("git", "log", "--diff-filter=M", "--name-only", "--format=", "-20")
	fileCmd.Dir = dir
	fileOut, _ := fileCmd.Output()
	topFile := mostFrequent(nonEmpty(strings.Split(strings.TrimSpace(string(fileOut)), "\n")))

	var suggestions []GitSuggestion

	// Suggestion 1: most-edited file.
	if topFile != "" {
		suggestions = append(suggestions, GitSuggestion{
			Text: fmt.Sprintf("How does %s work?", topFile),
		})
	}

	// Suggestion 2: recent commit message.
	if len(commits) > 0 {
		msg := commits[0]
		if len(msg) > 60 {
			msg = msg[:60] + "..."
		}
		suggestions = append(suggestions, GitSuggestion{
			Text: fmt.Sprintf("Explain the recent change: %s", msg),
		})
	}

	// Suggestion 3: write tests for the top file.
	if topFile != "" {
		suggestions = append(suggestions, GitSuggestion{
			Text: fmt.Sprintf("Write tests for %s", topFile),
		})
	}

	if len(suggestions) > 3 {
		suggestions = suggestions[:3]
	}
	return suggestions
}

// FormatGitSuggestions formats git suggestions into a welcome message block.
func FormatGitSuggestions(suggestions []GitSuggestion) string {
	if len(suggestions) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\nTry asking:\n")
	for _, s := range suggestions {
		sb.WriteString(fmt.Sprintf("  > %s\n", s.Text))
	}
	return sb.String()
}

func nonEmpty(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func mostFrequent(items []string) string {
	if len(items) == 0 {
		return ""
	}
	counts := make(map[string]int)
	for _, item := range items {
		counts[item]++
	}
	best := ""
	bestCount := 0
	for item, count := range counts {
		if count > bestCount {
			best = item
			bestCount = count
		}
	}
	return best
}
