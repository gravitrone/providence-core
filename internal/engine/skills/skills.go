package skills

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkillDefinition parsed from markdown frontmatter.
type SkillDefinition struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	WhenToUse    string   `yaml:"when_to_use"`
	AllowedTools []string `yaml:"allowed-tools"`
	Model        string   `yaml:"model"`
	Effort       string   `yaml:"effort"`
	Agent        string   `yaml:"agent"`
	Paths        string   `yaml:"paths"`
	// Prompt is the body markdown content after frontmatter.
	Prompt string `yaml:"-"`
	// FilePath is the source file on disk.
	FilePath string `yaml:"-"`
	// Source indicates where the skill was loaded from: "project", "user", or "builtin".
	Source string `yaml:"-"`
}

// searchDirs returns the ordered list of skill directories to scan.
// Project-level dirs come first so they override user-level.
func searchDirs(projectRoot, homeDir string) []struct {
	dir    string
	source string
} {
	return []struct {
		dir    string
		source string
	}{
		{filepath.Join(projectRoot, ".providence", "skills"), "project"},
		{filepath.Join(projectRoot, ".claude", "skills"), "project"},
		{filepath.Join(projectRoot, ".claude", "commands"), "project"}, // deprecated
		{filepath.Join(homeDir, ".providence", "skills"), "user"},
		{filepath.Join(homeDir, ".claude", "skills"), "user"},
	}
}

// LoadSkills discovers skills from multiple directories.
// Search order: project .providence/skills/, .claude/skills/, .claude/commands/ (deprecated),
// then user ~/.providence/skills/, ~/.claude/skills/.
// Project-level skills override user-level skills with the same name.
func LoadSkills(projectRoot, homeDir string) ([]SkillDefinition, error) {
	seen := make(map[string]struct{})
	var result []SkillDefinition

	for _, entry := range searchDirs(projectRoot, homeDir) {
		skills, err := loadSkillsFromDir(entry.dir, entry.source)
		if err != nil {
			return nil, fmt.Errorf("failed to load skills from %s: %w", entry.dir, err)
		}
		for _, s := range skills {
			if _, exists := seen[s.Name]; exists {
				continue
			}
			seen[s.Name] = struct{}{}
			result = append(result, s)
		}
	}

	return result, nil
}

// loadSkillsFromDir reads all .md files from a directory (non-recursive).
func loadSkillsFromDir(dir, source string) ([]SkillDefinition, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	var skills []SkillDefinition
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		skill, err := ParseSkillFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to parse skill %s: %w", path, err)
		}
		skill.Source = source
		// If name wasn't set in frontmatter, derive from filename.
		if skill.Name == "" {
			skill.Name = strings.TrimSuffix(e.Name(), ".md")
		}
		skills = append(skills, *skill)
	}

	return skills, nil
}

// ParseSkillFile reads a markdown file with optional YAML frontmatter.
// If the file starts with "---", the YAML between the first two "---" markers
// is parsed into SkillDefinition fields. Everything after is the Prompt.
// If there is no frontmatter, the entire file content becomes the Prompt.
func ParseSkillFile(path string) (*SkillDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	sd := &SkillDefinition{FilePath: path}
	content := string(data)

	if !strings.HasPrefix(strings.TrimSpace(content), "---") {
		sd.Prompt = strings.TrimSpace(content)
		return sd, nil
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	var (
		inFrontmatter bool
		frontmatter   strings.Builder
		body          strings.Builder
		pastFrontmatter bool
		lineNum       int
	)

	for scanner.Scan() {
		line := scanner.Text()
		lineNum++

		if lineNum == 1 && strings.TrimSpace(line) == "---" {
			inFrontmatter = true
			continue
		}

		if inFrontmatter && strings.TrimSpace(line) == "---" {
			inFrontmatter = false
			pastFrontmatter = true
			continue
		}

		if inFrontmatter {
			frontmatter.WriteString(line)
			frontmatter.WriteString("\n")
		} else if pastFrontmatter {
			body.WriteString(line)
			body.WriteString("\n")
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan file: %w", err)
	}

	if fm := frontmatter.String(); fm != "" {
		if err := yaml.Unmarshal([]byte(fm), sd); err != nil {
			return nil, fmt.Errorf("failed to parse frontmatter YAML: %w", err)
		}
	}

	sd.Prompt = strings.TrimSpace(body.String())
	return sd, nil
}
