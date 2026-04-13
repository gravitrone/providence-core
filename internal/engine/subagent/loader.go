package subagent

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// --- Loader ---

// agentSearchDirs returns the ordered list of agent directories to scan.
// Project-level dirs come first so they override user-level definitions.
func agentSearchDirs(projectRoot, homeDir string) []string {
	return []string{
		filepath.Join(projectRoot, ".providence", "agents"),
		filepath.Join(projectRoot, ".claude", "agents"),
		filepath.Join(homeDir, ".providence", "agents"),
		filepath.Join(homeDir, ".claude", "agents"),
	}
}

// LoadCustomAgents discovers agent definitions from markdown files in
// standard agent directories. Project-level agents override user-level
// agents with the same name (first found wins).
func LoadCustomAgents(projectRoot, homeDir string) (map[string]AgentType, error) {
	agents := make(map[string]AgentType)

	for _, dir := range agentSearchDirs(projectRoot, homeDir) {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			agent, err := ParseAgentFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				continue
			}
			// Derive name from filename if not set in frontmatter.
			if agent.Name == "" {
				agent.Name = strings.TrimSuffix(entry.Name(), ".md")
			}
			if _, exists := agents[agent.Name]; !exists {
				agents[agent.Name] = *agent
			}
		}
	}

	return agents, nil
}

// ParseAgentFile reads a markdown file with YAML frontmatter and returns
// an AgentType. The frontmatter fields map to AgentType struct tags. The
// markdown body after frontmatter becomes the SystemPrompt.
func ParseAgentFile(path string) (*AgentType, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent file: %w", err)
	}

	at := &AgentType{}
	content := string(data)

	// No frontmatter - entire file is the system prompt.
	if !strings.HasPrefix(strings.TrimSpace(content), "---") {
		at.SystemPrompt = strings.TrimSpace(content)
		return at, nil
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
		return nil, fmt.Errorf("failed to scan agent file: %w", err)
	}

	if fm := frontmatter.String(); fm != "" {
		if err := yaml.Unmarshal([]byte(fm), at); err != nil {
			return nil, fmt.Errorf("failed to parse agent frontmatter YAML: %w", err)
		}
	}

	at.SystemPrompt = strings.TrimSpace(body.String())
	return at, nil
}
