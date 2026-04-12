package outputstyles

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// OutputStyle loaded from .providence/output-styles/*.md.
type OutputStyle struct {
	Name                   string `yaml:"name"`
	Description            string `yaml:"description"`
	KeepCodingInstructions bool   `yaml:"keep-coding-instructions"`
	// Prompt is the markdown body after frontmatter.
	Prompt string `yaml:"-"`
	// FilePath is the source file on disk.
	FilePath string `yaml:"-"`
}

// LoadOutputStyles discovers output styles from project and user directories.
// Project-level styles override user-level styles with the same name.
func LoadOutputStyles(projectRoot, homeDir string) ([]OutputStyle, error) {
	dirs := []string{
		filepath.Join(projectRoot, ".providence", "output-styles"),
		filepath.Join(projectRoot, ".claude", "output-styles"),
		filepath.Join(homeDir, ".providence", "output-styles"),
		filepath.Join(homeDir, ".claude", "output-styles"),
	}

	seen := make(map[string]struct{})
	var result []OutputStyle

	for _, dir := range dirs {
		styles, err := loadStylesFromDir(dir)
		if err != nil {
			return nil, fmt.Errorf("failed to load output styles from %s: %w", dir, err)
		}
		for _, s := range styles {
			if _, exists := seen[s.Name]; exists {
				continue
			}
			seen[s.Name] = struct{}{}
			result = append(result, s)
		}
	}

	return result, nil
}

// loadStylesFromDir reads all .md files from a directory (non-recursive).
func loadStylesFromDir(dir string) ([]OutputStyle, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	var styles []OutputStyle
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		style, err := ParseStyleFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to parse style %s: %w", path, err)
		}
		if style.Name == "" {
			style.Name = strings.TrimSuffix(e.Name(), ".md")
		}
		styles = append(styles, *style)
	}

	return styles, nil
}

// ParseStyleFile reads a markdown file with optional YAML frontmatter.
// Same pattern as skills: frontmatter between --- markers, body after.
func ParseStyleFile(path string) (*OutputStyle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	style := &OutputStyle{FilePath: path}
	content := string(data)

	if !strings.HasPrefix(strings.TrimSpace(content), "---") {
		style.Prompt = strings.TrimSpace(content)
		return style, nil
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	var (
		inFrontmatter   bool
		frontmatter     strings.Builder
		body            strings.Builder
		pastFrontmatter bool
		lineNum         int
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
		if err := yaml.Unmarshal([]byte(fm), style); err != nil {
			return nil, fmt.Errorf("failed to parse frontmatter yaml: %w", err)
		}
	}

	style.Prompt = strings.TrimSpace(body.String())
	return style, nil
}
