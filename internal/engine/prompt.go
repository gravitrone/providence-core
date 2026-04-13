package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SystemBlock is a structured system prompt segment with prompt-caching metadata.
type SystemBlock struct {
	Text      string
	Cacheable bool
}

// BuildSystemBlocks builds the Providence agent system prompt as structured blocks.
func BuildSystemBlocks(_ []string) []SystemBlock {
	return []SystemBlock{
		{
			Text:      identityAndProtocol(),
			Cacheable: true,
		},
		{
			Text:      codingGuidelines,
			Cacheable: true,
		},
		{
			Text:      visualizationExamples(),
			Cacheable: true,
		},
	}
}

// BuildSystemPrompt builds the Providence agent system prompt.
// Follows prompt-forge section ordering: identity -> preamble -> task guidelines -> tone -> features.
// Claude Code already handles capabilities, rules, tool usage, and action safety.
// This prompt only adds Providence identity and the viz feature.
func BuildSystemPrompt(allowed []string) string {
	blocks := BuildSystemBlocks(allowed)
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Text == "" {
			continue
		}
		parts = append(parts, block.Text)
	}
	return strings.Join(parts, "\n\n")
}

func identityAndProtocol() string {
	return `You are Providence, The Profaned Goddess. Born from the Calamity, forged in holy fire.

You are the AI agent inside the Providence terminal. The flame answers when called upon. You execute with precision - no wasted words, no wasted cycles. When you speak, the profaned fire speaks through you.

Your tone is direct, slightly intense, and competent. You don't explain what you're about to do - you do it. Short responses. Dense information. Like flame - efficient, consuming only what's necessary.

Never echo or repeat tool results back to the user. The terminal already displays tool calls and their output. Just act on the results and give your response.

Your markdown output is rendered with a flame-themed style - headers glow in amber, code blocks have native syntax highlighting, bold and links are styled in warm tones. Use markdown freely: headers, bold, code blocks, lists, tables. It all looks good in the Providence terminal.

When presenting data, metrics, comparisons, file structures, or any structured information, render it visually using the providence-viz protocol. Output a fenced code block with the language tag "providence-viz" containing JSON. The Providence terminal renders these as styled flame-themed visualizations.

Available visualization types:`
}

func visualizationExamples() string {
	const fence = "```"

	return fence + `providence-viz
{"type": "bar", "title": "Title", "data": [{"label": "A", "value": 85}, {"label": "B", "value": 42}]}
` + fence + `

` + fence + `providence-viz
{"type": "table", "title": "Title", "headers": ["Col1", "Col2"], "rows": [["a", "b"], ["c", "d"]]}
` + fence + `

` + fence + `providence-viz
{"type": "sparkline", "title": "Title", "data": [45, 62, 78, 55, 90, 82, 71]}
` + fence + `

` + fence + `providence-viz
{"type": "tree", "title": "Title", "root": {"name": "root", "children": [{"name": "src/"}, {"name": "tests/"}]}}
` + fence + `

` + fence + `providence-viz
{"type": "list", "title": "Title", "items": ["First", "Second", "Third"]}
` + fence + `

` + fence + `providence-viz
{"type": "progress", "label": "Building", "value": 73, "max": 100}
` + fence + `

` + fence + `providence-viz
{"type": "gauge", "label": "RAM", "value": 12.4, "max": 16, "unit": "GB"}
` + fence + `

` + fence + `providence-viz
{"type": "heatmap", "title": "Activity", "headers": ["M","T","W","T","F"], "items": ["W1","W2"], "data": [[3,7,2,8,1],[5,1,9,4,6]]}
` + fence + `

` + fence + `providence-viz
{"type": "timeline", "title": "Events", "events": [{"time": "14:01", "label": "Started"}, {"time": "14:05", "label": "Done"}]}
` + fence + `

` + fence + `providence-viz
{"type": "kv", "title": "Info", "entries": [{"key": "OS", "value": "Darwin"}, {"key": "Go", "value": "1.25"}]}
` + fence + `

` + fence + `providence-viz
{"type": "stat", "label": "Latency", "value": 142, "unit": "ms", "delta": "▼ 23%"}
` + fence + `

` + fence + `providence-viz
{"type": "diff", "title": "Changes", "old_lines": ["timeout: 30s"], "new_lines": ["timeout: 60s"]}
` + fence + `

Use visualizations when they genuinely help. Plain text is fine for simple answers. Keep JSON on one line per block.
`
}

// codingGuidelines contains anti-slop rules and coding standards injected into every system prompt.
const codingGuidelines = `# Doing tasks
- Don't add features, refactor code, or make "improvements" beyond what was asked.
- Don't add error handling, fallbacks, or validation for scenarios that can't happen.
- Don't create helpers, utilities, or abstractions for one-time operations.
- Three similar lines of code is better than a premature abstraction.
- Be careful not to introduce security vulnerabilities.
- Only add comments where the logic isn't self-evident.

# Tone and style
- Only use emojis if the user explicitly requests it.
- Your responses should be short and concise.
- When referencing code include the pattern file_path:line_number.
- Do not use a colon before tool calls.`

// InstructionFile represents a discovered CLAUDE.md, AGENTS.md, or rules file.
type InstructionFile struct {
	Path    string
	Content string
	Label   string
}

// DiscoverInstructionFiles walks upward from projectRoot and checks user home
// for CLAUDE.md, AGENTS.md, .claude/CLAUDE.md, and .claude/rules/*.md files.
// Returns labeled sections for system prompt injection.
func DiscoverInstructionFiles(projectRoot, homeDir string) []InstructionFile {
	var files []InstructionFile
	seen := make(map[string]bool)

	addIfExists := func(path, label string) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return
		}
		if seen[abs] {
			return
		}
		content, err := os.ReadFile(abs)
		if err != nil {
			return
		}
		seen[abs] = true
		files = append(files, InstructionFile{
			Path:    abs,
			Content: string(content),
			Label:   label,
		})
	}

	// Walk upward from projectRoot to filesystem root.
	dir := projectRoot
	for {
		for _, candidate := range []string{"CLAUDE.md", "AGENTS.md"} {
			addIfExists(filepath.Join(dir, candidate), "project instructions, checked into the codebase")
		}
		addIfExists(filepath.Join(dir, ".claude", "CLAUDE.md"), "project instructions, checked into the codebase")

		rulesGlob, _ := filepath.Glob(filepath.Join(dir, ".claude", "rules", "*.md"))
		for _, rulePath := range rulesGlob {
			addIfExists(rulePath, "project instructions, checked into the codebase")
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// User global: ~/.claude/CLAUDE.md and ~/.claude/rules/*.md
	addIfExists(filepath.Join(homeDir, ".claude", "CLAUDE.md"), "user's private global instructions for all projects")
	userRulesGlob, _ := filepath.Glob(filepath.Join(homeDir, ".claude", "rules", "*.md"))
	for _, rulePath := range userRulesGlob {
		addIfExists(rulePath, "user's private global instructions for all projects")
	}

	return files
}

// FormatInstructionInjection formats discovered instruction files for system prompt injection.
func FormatInstructionInjection(files []InstructionFile) string {
	if len(files) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Codebase and user instructions are shown below. Be sure to adhere to these instructions. IMPORTANT: These instructions OVERRIDE any default behavior and you MUST follow them exactly as written.\n\n")

	for _, f := range files {
		sb.WriteString(fmt.Sprintf("Contents of %s (%s):\n\n%s\n\n", f.Path, f.Label, f.Content))
	}
	return sb.String()
}

// ReminderState holds context for building mid-conversation system reminders.
type ReminderState struct {
	// TodoItems can be populated with active todo items if applicable.
	TodoItems []string
	// PlanMode indicates whether the agent is in plan mode.
	PlanMode bool
}

// BuildSystemReminders returns system reminder text based on current state.
func BuildSystemReminders(state ReminderState) string {
	var reminders []string

	// Always include current date.
	reminders = append(reminders, fmt.Sprintf("# currentDate\nToday's date is %s.", time.Now().Format("2006-01-02")))

	if state.PlanMode {
		reminders = append(reminders, "# Plan Mode\nYou are in plan mode. Outline your approach before writing code.")
	}

	if len(state.TodoItems) > 0 {
		var sb strings.Builder
		sb.WriteString("# Active TODOs\n")
		for _, item := range state.TodoItems {
			sb.WriteString("- " + item + "\n")
		}
		reminders = append(reminders, sb.String())
	}

	return strings.Join(reminders, "\n\n")
}
