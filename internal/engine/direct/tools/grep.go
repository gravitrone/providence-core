package tools

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultHeadLimit = 250
	maxGrepChars     = 20_000
)

// GrepTool searches file contents using regex patterns.
// Shells out to rg (ripgrep) if available on PATH, falls back to Go regex.
type GrepTool struct{}

func NewGrepTool() *GrepTool    { return &GrepTool{} }
func (g *GrepTool) Name() string { return "Grep" }
func (g *GrepTool) Description() string {
	return "Search file contents using regex patterns."
}
func (g *GrepTool) ReadOnly() bool { return true }

// Prompt implements ToolPrompter with CC-parity guidance for content search.
func (g *GrepTool) Prompt() string {
	return `A powerful search tool built on ripgrep.

Usage:
- ALWAYS use Grep for search tasks. NEVER invoke grep or rg as a Bash command. The Grep tool has been optimized for correct permissions and access.
- Supports full regex syntax (e.g., "log.*Error", "function\s+\w+")
- Filter files with glob parameter (e.g., "*.js", "**/*.tsx") or type parameter (e.g., "js", "py", "rust")
- Output modes: "content" shows matching lines, "files_with_matches" shows only file paths (default), "count" shows match counts
- Use Agent tool for open-ended searches requiring multiple rounds
- Pattern syntax: Uses ripgrep (not grep) - literal braces need escaping (use ` + "`interface\\{\\}`" + ` to find ` + "`interface{}`" + ` in Go code)
- Multiline matching: By default patterns match within single lines only. For cross-line patterns like ` + "`struct \\{[\\s\\S]*?field`" + `, use multiline: true`
}

func (g *GrepTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regular expression pattern to search for.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "File or directory to search in (defaults to \".\").",
			},
			"output_mode": map[string]any{
				"type":        "string",
				"description": "Output mode: files_with_matches (default), content, count.",
				"enum":        []string{"files_with_matches", "content", "count"},
			},
			"head_limit": map[string]any{
				"type":        "integer",
				"description": "Max entries to return (default 250). Pass 0 for unlimited.",
			},
			"glob": map[string]any{
				"type":        "string",
				"description": "Glob pattern to filter files (e.g. \"*.go\").",
			},
			"-A": map[string]any{
				"type":        "integer",
				"description": "Lines to show after each match (content mode only).",
			},
			"-B": map[string]any{
				"type":        "integer",
				"description": "Lines to show before each match (content mode only).",
			},
			"-C": map[string]any{
				"type":        "integer",
				"description": "Context lines before and after each match (alias for -A + -B).",
			},
			"-i": map[string]any{
				"type":        "boolean",
				"description": "Case insensitive search.",
			},
			"-n": map[string]any{
				"type":        "boolean",
				"description": "Show line numbers (default true for content mode).",
			},
			"multiline": map[string]any{
				"type":        "boolean",
				"description": "Enable multiline mode (pattern matches across lines).",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Skip first N entries before applying head_limit.",
			},
			"type": map[string]any{
				"type":        "string",
				"description": "File type filter (e.g. \"go\", \"py\", \"js\"). Maps to rg --type.",
			},
		},
		"required": []string{"pattern"},
	}
}

// rgAvailable checks if ripgrep (rg) is on PATH.
func rgAvailable() bool {
	_, err := exec.LookPath("rg")
	return err == nil
}

func (g *GrepTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	pattern := paramString(input, "pattern", "")
	if pattern == "" {
		return ToolResult{Content: "pattern is required", IsError: true}
	}

	root := paramString(input, "path", ".")
	root = filepath.Clean(root)
	mode := paramString(input, "output_mode", "files_with_matches")
	headLimit := paramInt(input, "head_limit", defaultHeadLimit)
	fileGlob := paramString(input, "glob", "")
	afterCtx := paramInt(input, "-A", 0)
	beforeCtx := paramInt(input, "-B", 0)
	contextLines := paramInt(input, "-C", 0)
	caseInsensitive := paramBool(input, "-i", false)
	showLineNums := paramBool(input, "-n", true)
	multiline := paramBool(input, "multiline", false)
	offset := paramInt(input, "offset", 0)
	fileType := paramString(input, "type", "")

	// -C is shorthand for both -A and -B.
	if contextLines > 0 {
		if afterCtx == 0 {
			afterCtx = contextLines
		}
		if beforeCtx == 0 {
			beforeCtx = contextLines
		}
	}

	// Try ripgrep first.
	if rgAvailable() {
		return g.executeRg(ctx, pattern, root, mode, headLimit, fileGlob,
			afterCtx, beforeCtx, caseInsensitive, showLineNums, multiline, offset, fileType)
	}

	// Fallback: Go regex implementation (no context lines, limited features).
	return g.executeFallback(ctx, pattern, root, mode, headLimit, fileGlob,
		caseInsensitive, offset)
}

// executeRg runs the search via ripgrep.
func (g *GrepTool) executeRg(ctx context.Context, pattern, root, mode string,
	headLimit int, fileGlob string, afterCtx, beforeCtx int,
	caseInsensitive, showLineNums, multiline bool, offset int, fileType string,
) ToolResult {
	args := []string{"--no-heading", "--color=never"}

	switch mode {
	case "files_with_matches":
		args = append(args, "--files-with-matches")
	case "count":
		args = append(args, "--count")
	case "content":
		if showLineNums {
			args = append(args, "--line-number")
		}
		if afterCtx > 0 {
			args = append(args, "-A", strconv.Itoa(afterCtx))
		}
		if beforeCtx > 0 {
			args = append(args, "-B", strconv.Itoa(beforeCtx))
		}
	}

	if caseInsensitive {
		args = append(args, "-i")
	}
	if multiline {
		args = append(args, "--multiline", "--multiline-dotall")
	}
	if fileType != "" {
		args = append(args, "--type", fileType)
	}
	if fileGlob != "" {
		args = append(args, "--glob", fileGlob)
	}

	args = append(args, pattern, root)

	cmd := exec.CommandContext(ctx, "rg", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	// rg exits 1 when no matches found, not an error.
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return ToolResult{Content: ""}
			}
			if exitErr.ExitCode() == 2 {
				return ToolResult{Content: fmt.Sprintf("rg error: %s", stderr.String()), IsError: true}
			}
		} else {
			return ToolResult{Content: fmt.Sprintf("rg error: %v", err), IsError: true}
		}
	}

	output := stdout.String()
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return ToolResult{Content: ""}
	}

	// Apply offset.
	if offset > 0 && offset < len(lines) {
		lines = lines[offset:]
	} else if offset >= len(lines) {
		return ToolResult{Content: ""}
	}

	// Apply head limit.
	if headLimit > 0 && len(lines) > headLimit {
		lines = lines[:headLimit]
	}

	result := strings.Join(lines, "\n")
	if len(result) > maxGrepChars {
		result = result[:maxGrepChars] + "\n... truncated"
	}

	return ToolResult{Content: result}
}

// executeFallback uses Go's regexp for searching when rg is unavailable.
func (g *GrepTool) executeFallback(ctx context.Context, pattern, root, mode string,
	headLimit int, fileGlob string, caseInsensitive bool, offset int,
) ToolResult {
	if caseInsensitive {
		pattern = "(?i)" + pattern
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid regex: %v", err), IsError: true}
	}

	info, err := os.Stat(root)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("path error: %v", err), IsError: true}
	}

	var files []string
	if !info.IsDir() {
		files = []string{root}
	} else {
		files = collectFiles(root, fileGlob)
	}

	switch mode {
	case "files_with_matches":
		return grepFilesWithMatches(re, files, headLimit, offset)
	case "content":
		return grepContent(re, files, headLimit, offset)
	case "count":
		return grepCount(re, files, headLimit, offset)
	default:
		return grepFilesWithMatches(re, files, headLimit, offset)
	}
}

// collectFiles walks a directory, filtering by glob and skipping excluded dirs.
// Stops after 10k files to prevent hanging on large directories.
func collectFiles(root, fileGlob string) []string {
	var files []string
	count := 0
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		count++
		if count > 10000 {
			return filepath.SkipAll
		}
		if skipFiles[info.Name()] {
			return nil
		}
		if fileGlob != "" {
			matched, _ := filepath.Match(fileGlob, info.Name())
			if !matched {
				return nil
			}
		}
		files = append(files, path)
		return nil
	})
	return files
}

func grepFilesWithMatches(re *regexp.Regexp, files []string, limit, offset int) ToolResult {
	var matches []string
	totalChars := 0
	skipped := 0

	for _, path := range files {
		if len(matches) >= limit {
			break
		}
		if matchesInFile(re, path) {
			if skipped < offset {
				skipped++
				continue
			}
			matches = append(matches, path)
			totalChars += len(path) + 1
			if totalChars > maxGrepChars {
				break
			}
		}
	}

	return ToolResult{Content: strings.Join(matches, "\n")}
}

func grepContent(re *regexp.Regexp, files []string, limit, offset int) ToolResult {
	var b strings.Builder
	entries := 0
	skipped := 0

	for _, path := range files {
		if entries >= limit || b.Len() >= maxGrepChars {
			break
		}

		f, err := os.Open(path)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
		lineNum := 0

		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				if skipped < offset {
					skipped++
					continue
				}
				entry := fmt.Sprintf("%s:%d:%s\n", path, lineNum, line)
				if b.Len()+len(entry) > maxGrepChars {
					f.Close()
					b.WriteString("\n... truncated\n")
					return ToolResult{Content: b.String()}
				}
				b.WriteString(entry)
				entries++
				if entries >= limit {
					f.Close()
					return ToolResult{Content: b.String()}
				}
			}
		}
		f.Close()
	}

	return ToolResult{Content: b.String()}
}

func grepCount(re *regexp.Regexp, files []string, limit, offset int) ToolResult {
	type countEntry struct {
		path  string
		count int
	}

	var results []countEntry

	for _, path := range files {
		c := countMatchesInFile(re, path)
		if c > 0 {
			results = append(results, countEntry{path: path, count: c})
		}
	}

	// Sort by count descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].count > results[j].count
	})

	// Apply offset.
	if offset > 0 && offset < len(results) {
		results = results[offset:]
	} else if offset > 0 {
		results = nil
	}

	if len(results) > limit {
		results = results[:limit]
	}

	var b strings.Builder
	for _, r := range results {
		entry := fmt.Sprintf("%s:%d\n", r.path, r.count)
		if b.Len()+len(entry) > maxGrepChars {
			break
		}
		b.WriteString(entry)
	}

	return ToolResult{Content: b.String()}
}

func matchesInFile(re *regexp.Regexp, path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	// Quick binary check on first chunk.
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n > 0 {
		for _, b := range buf[:n] {
			if b == 0 {
				return false // binary file, skip
			}
		}
	}
	f.Seek(0, 0)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
	for scanner.Scan() {
		if re.MatchString(scanner.Text()) {
			return true
		}
	}
	return false
}

func countMatchesInFile(re *regexp.Regexp, path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	// Skip binary.
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n > 0 {
		for _, b := range buf[:n] {
			if b == 0 {
				return 0
			}
		}
	}
	f.Seek(0, 0)

	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
	for scanner.Scan() {
		if re.MatchString(scanner.Text()) {
			count++
		}
	}
	return count
}
