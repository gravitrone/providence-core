package direct

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// extractSessionMemory builds a markdown memory entry from the conversation
// history. Purely mechanical - no LLM calls. Returns empty string if there
// is not enough content to be worth recording.
func (e *DirectEngine) extractSessionMemory() string {
	msgs := e.history.Messages()

	// Count user turns (each user message = 1 turn).
	userTurns := 0
	for _, msg := range msgs {
		if msg.Role == "user" {
			userTurns++
		}
	}
	if userTurns < 5 {
		return ""
	}

	var (
		filesModified []string
		toolCounts    = map[string]int{}
		decisions     []string
		firstUserMsg  string
	)

	modifiedSet := map[string]bool{}

	for _, msg := range msgs {
		// Collect key user decisions.
		if msg.Role == "user" {
			for _, block := range msg.Content {
				if block.OfText == nil {
					continue
				}
				text := strings.TrimSpace(block.OfText.Text)
				if firstUserMsg == "" && text != "" {
					firstUserMsg = text
				}
				lower := strings.ToLower(text)
				if isDecision(lower) && len(decisions) < 3 {
					if len(text) > 80 {
						text = text[:80] + "..."
					}
					decisions = append(decisions, text)
				}
			}
		}

		// Scan assistant tool_use blocks.
		if msg.Role == "assistant" {
			for _, block := range msg.Content {
				tu := block.OfToolUse
				if tu == nil {
					continue
				}
				toolCounts[tu.Name]++

				// Extract file paths from Edit/Write tool calls.
				if tu.Name == "Edit" || tu.Name == "Write" {
					if fp := extractInputField(tu.Input, "file_path"); fp != "" {
						if !modifiedSet[fp] {
							modifiedSet[fp] = true
							filesModified = append(filesModified, fp)
						}
					}
				}
			}
		}
	}

	// Build the title from first user message.
	title := truncateTitle(firstUserMsg, 60)
	if title == "" {
		title = "untitled session"
	}

	date := time.Now().Format("2006-01-02")

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n## Session %s - %s\n", date, title))

	if len(filesModified) > 0 {
		sb.WriteString(fmt.Sprintf("- Files modified: %s\n", strings.Join(filesModified, ", ")))
	}

	if len(toolCounts) > 0 {
		parts := make([]string, 0, len(toolCounts))
		for name, count := range toolCounts {
			parts = append(parts, fmt.Sprintf("%s(%d)", name, count))
		}
		sb.WriteString(fmt.Sprintf("- Tools used: %s\n", strings.Join(parts, ", ")))
	}

	if len(decisions) > 0 {
		sb.WriteString(fmt.Sprintf("- Key decisions: %s\n", strings.Join(decisions, "; ")))
	}

	return sb.String()
}

// appendSessionMemory writes the memory entry to the project MEMORY.md file.
// Creates directories if needed. Silently ignores errors.
func (e *DirectEngine) appendSessionMemory() {
	entry := e.extractSessionMemory()
	if entry == "" {
		return
	}

	slug := projectSlug(e.workDir)
	if slug == "" {
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	memDir := filepath.Join(home, ".claude", "projects", slug, "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		return
	}

	memFile := filepath.Join(memDir, "MEMORY.md")

	f, err := os.OpenFile(memFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	_, _ = f.WriteString(entry)
}

// --- Helpers ---

// isDecision returns true if the message looks like a user decision.
func isDecision(lower string) bool {
	prefixes := []string{
		"yes", "no", "let's", "lets", "don't", "dont",
		"do it", "go ahead", "skip", "use ", "switch ",
		"keep ", "remove ", "add ", "drop ",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// truncateTitle trims a string to maxLen, cutting at word boundary.
func truncateTitle(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	// Replace newlines with spaces.
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	// Cut at last space before maxLen.
	cut := s[:maxLen]
	if idx := strings.LastIndex(cut, " "); idx > maxLen/2 {
		cut = cut[:idx]
	}
	return cut + "..."
}

// projectSlug computes a slug from a working directory path.
// Strips home prefix, replaces path separators with dashes.
func projectSlug(workDir string) string {
	if workDir == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	slug := workDir
	if strings.HasPrefix(slug, home) {
		slug = slug[len(home):]
	}
	slug = strings.TrimPrefix(slug, "/")
	slug = strings.ReplaceAll(slug, "/", "-")
	return slug
}

// extractInputField pulls a string field from a tool_use Input.
func extractInputField(input any, key string) string {
	var m map[string]any
	switch v := input.(type) {
	case map[string]any:
		m = v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		if err := json.Unmarshal(data, &m); err != nil {
			return ""
		}
	}
	if val, ok := m[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}
