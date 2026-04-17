package outputstyles

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const builtinDefault = ``

const builtinExplanatory = `Explain your decisions as you work so the user can follow the tradeoffs and sequence.
Keep the task moving, but briefly call out why you picked an approach, check, or edit when that context helps someone learn the codebase.`

const builtinLearning = `Treat each task as a teaching moment and leave the user with a clear path to continue the work themselves.
When you add or reshape code, include short TODO(human) stubs where the user should fill in project-specific judgment or follow-up details.`

// OutputStyle describes a built-in or disk-loaded output style.
type OutputStyle struct {
	Name                   string `yaml:"name"`
	Description            string `yaml:"description"`
	KeepCodingInstructions bool   `yaml:"keep-coding-instructions"`
	// Prompt is the markdown body after frontmatter.
	Prompt string `yaml:"-"`
	// FilePath is the source file on disk.
	FilePath string `yaml:"-"`
}

func builtinStyles() []OutputStyle {
	return []OutputStyle{
		{
			Name:        "default",
			Description: "Providence baseline output style.",
			Prompt:      builtinDefault,
		},
		{
			Name:        "explanatory",
			Description: "Explains decisions while working through the task.",
			Prompt:      builtinExplanatory,
		},
		{
			Name:        "learning",
			Description: "Turns tasks into short teaching moments with TODO(human) stubs.",
			Prompt:      builtinLearning,
		},
	}
}

// LoadOutputStyles discovers built-in, project, and user output styles.
// Disk-loaded styles override built-ins with the same name.
// Project-level styles override user-level styles with the same name.
func LoadOutputStyles(projectRoot, homeDir string) ([]OutputStyle, error) {
	dirs := []string{
		filepath.Join(projectRoot, ".providence", "output-styles"),
		filepath.Join(projectRoot, ".claude", "output-styles"),
		filepath.Join(homeDir, ".providence", "output-styles"),
		filepath.Join(homeDir, ".claude", "output-styles"),
	}

	result := builtinStyles()
	indexByName := make(map[string]int, len(result))
	for i := range result {
		indexByName[result[i].Name] = i
	}

	diskSeen := make(map[string]struct{})

	for _, dir := range dirs {
		styles, err := loadStylesFromDir(dir)
		if err != nil {
			return nil, fmt.Errorf("failed to load output styles from %s: %w", dir, err)
		}
		for _, s := range styles {
			if _, exists := diskSeen[s.Name]; exists {
				continue
			}

			if idx, exists := indexByName[s.Name]; exists {
				result[idx] = s
			} else {
				indexByName[s.Name] = len(result)
				result = append(result, s)
			}
			diskSeen[s.Name] = struct{}{}
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
