package skills

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ActivatedSkill describes a conditional skill activated by a file path match.
type ActivatedSkill struct {
	Name         string
	Instructions string
	MatchedGlob  string
}

type conditionalSkillFrontmatter struct {
	PathGlobs []string `yaml:"path_globs"`
}

// ActivateForPaths returns the loaded skills whose path_globs match at least
// one of the provided paths.
func ActivateForPaths(paths []string) []ActivatedSkill {
	projectRoot, err := os.Getwd()
	if err != nil {
		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = ""
	}

	definitions, err := LoadSkills(projectRoot, homeDir)
	if err != nil {
		return nil
	}

	return activateForDefinitions(definitions, projectRoot, paths)
}

func activateForDefinitions(definitions []SkillDefinition, projectRoot string, paths []string) []ActivatedSkill {
	candidates := normalizeActivationPaths(projectRoot, paths)
	if len(candidates) == 0 {
		return nil
	}

	activated := make([]ActivatedSkill, 0)
	seen := make(map[string]struct{})
	for _, definition := range definitions {
		if _, exists := seen[definition.Name]; exists {
			continue
		}

		pathGlobs := loadConditionalPathGlobs(definition.FilePath)
		matchedGlob := matchConditionalSkill(pathGlobs, candidates)
		if matchedGlob == "" {
			continue
		}

		activated = append(activated, ActivatedSkill{
			Name:         definition.Name,
			Instructions: definition.Prompt,
			MatchedGlob:  matchedGlob,
		})
		seen[definition.Name] = struct{}{}
	}

	return activated
}

func normalizeActivationPaths(projectRoot string, paths []string) []string {
	candidates := make([]string, 0, len(paths)*2)
	seen := make(map[string]struct{})

	addCandidate := func(path string) {
		if path == "" {
			return
		}
		if _, exists := seen[path]; exists {
			return
		}
		seen[path] = struct{}{}
		candidates = append(candidates, path)
	}

	for _, path := range paths {
		cleanedPath := filepath.Clean(path)
		addCandidate(cleanedPath)

		if projectRoot == "" || !filepath.IsAbs(cleanedPath) {
			continue
		}

		relativePath, err := filepath.Rel(projectRoot, cleanedPath)
		if err != nil || relativePath == "." {
			continue
		}
		if relativePath == ".." {
			continue
		}
		if strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
			continue
		}

		addCandidate(relativePath)
	}

	return candidates
}

func loadConditionalPathGlobs(path string) []string {
	frontmatter, ok := extractConditionalFrontmatter(path)
	if !ok {
		return nil
	}

	var manifest conditionalSkillFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatter), &manifest); err != nil {
		return nil
	}

	return manifest.PathGlobs
}

func extractConditionalFrontmatter(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}

	content := string(data)
	scanner := bufio.NewScanner(strings.NewReader(content))

	var frontmatter strings.Builder
	lineNum := 0
	inFrontmatter := false
	for scanner.Scan() {
		line := scanner.Text()
		lineNum++

		if lineNum == 1 {
			if strings.TrimSpace(line) != "---" {
				return "", false
			}
			inFrontmatter = true
			continue
		}

		if inFrontmatter && strings.TrimSpace(line) == "---" {
			return frontmatter.String(), true
		}

		if inFrontmatter {
			frontmatter.WriteString(line)
			frontmatter.WriteString("\n")
		}
	}

	return "", false
}

func matchConditionalSkill(pathGlobs []string, paths []string) string {
	for _, pathGlob := range pathGlobs {
		trimmedGlob := strings.TrimSpace(pathGlob)
		if trimmedGlob == "" {
			continue
		}

		for _, path := range paths {
			matched, err := filepath.Match(trimmedGlob, path)
			if err == nil && matched {
				return trimmedGlob
			}
		}
	}

	return ""
}
