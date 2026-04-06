package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	defaultHeadLimit = 250
	maxGrepChars     = 20_000
)

// GrepTool searches file contents using regex patterns.
type GrepTool struct{}

func NewGrepTool() *GrepTool    { return &GrepTool{} }
func (g *GrepTool) Name() string { return "Grep" }
func (g *GrepTool) Description() string {
	return "Search file contents using regex patterns."
}
func (g *GrepTool) ReadOnly() bool { return true }

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
				"description": "Max entries to return (default 250).",
			},
			"glob": map[string]any{
				"type":        "string",
				"description": "Glob pattern to filter files (e.g. \"*.go\").",
			},
		},
		"required": []string{"pattern"},
	}
}

func (g *GrepTool) Execute(_ context.Context, input map[string]any) ToolResult {
	pattern := paramString(input, "pattern", "")
	if pattern == "" {
		return ToolResult{Content: "pattern is required", IsError: true}
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid regex: %v", err), IsError: true}
	}

	root := paramString(input, "path", ".")
	root = filepath.Clean(root)
	mode := paramString(input, "output_mode", "files_with_matches")
	headLimit := paramInt(input, "head_limit", defaultHeadLimit)
	fileGlob := paramString(input, "glob", "")

	// check if root is a file
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
		return grepFilesWithMatches(re, files, headLimit)
	case "content":
		return grepContent(re, files, headLimit)
	case "count":
		return grepCount(re, files, headLimit)
	default:
		return grepFilesWithMatches(re, files, headLimit)
	}
}

// collectFiles walks a directory, filtering by glob and skipping excluded dirs.
func collectFiles(root, fileGlob string) []string {
	var files []string
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

func grepFilesWithMatches(re *regexp.Regexp, files []string, limit int) ToolResult {
	var matches []string
	totalChars := 0

	for _, path := range files {
		if len(matches) >= limit {
			break
		}
		if matchesInFile(re, path) {
			matches = append(matches, path)
			totalChars += len(path) + 1
			if totalChars > maxGrepChars {
				break
			}
		}
	}

	return ToolResult{Content: strings.Join(matches, "\n")}
}

func grepContent(re *regexp.Regexp, files []string, limit int) ToolResult {
	var b strings.Builder
	entries := 0

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

func grepCount(re *regexp.Regexp, files []string, limit int) ToolResult {
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

	// sort by count descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].count > results[j].count
	})

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

	// quick binary check on first chunk
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n > 0 {
		for _, b := range buf[:n] {
			if b == 0 {
				return false // skip binary
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

	// skip binary
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
