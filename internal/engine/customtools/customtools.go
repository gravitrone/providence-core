package customtools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultTimeout is the maximum execution time for a custom tool command.
const DefaultTimeout = 120 * time.Second

// ToolManifest parsed from tool.yaml.
type ToolManifest struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Inputs      map[string]Input `yaml:"inputs"`
	Command     string           `yaml:"command"`
	Stdin       string           `yaml:"stdin"` // "json" or empty
}

// Input describes a single tool input parameter.
type Input struct {
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
}

// LoadCustomTools discovers tools from .providence/tools/ and ~/.providence/tools/.
// Project-level tools override user-level tools with the same name.
func LoadCustomTools(projectRoot, homeDir string) ([]CustomTool, error) {
	dirs := []struct {
		dir    string
		source string
	}{
		{filepath.Join(projectRoot, ".providence", "tools"), "project"},
		{filepath.Join(homeDir, ".providence", "tools"), "user"},
	}

	seen := make(map[string]struct{})
	var result []CustomTool

	for _, entry := range dirs {
		tools, err := loadToolsFromDir(entry.dir)
		if err != nil {
			return nil, fmt.Errorf("failed to load tools from %s: %w", entry.dir, err)
		}
		for _, tool := range tools {
			if _, exists := seen[tool.Manifest.Name]; exists {
				continue
			}
			seen[tool.Manifest.Name] = struct{}{}
			result = append(result, tool)
		}
	}

	return result, nil
}

// loadToolsFromDir scans a tools directory for subdirectories containing tool.yaml.
func loadToolsFromDir(dir string) ([]CustomTool, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	var tools []CustomTool
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		manifestPath := filepath.Join(dir, e.Name(), "tool.yaml")
		manifest, err := ParseToolManifest(manifestPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse tool manifest %s: %w", manifestPath, err)
		}
		// If name not set in manifest, derive from directory name.
		if manifest.Name == "" {
			manifest.Name = e.Name()
		}
		tools = append(tools, CustomTool{
			Manifest: *manifest,
			Dir:      filepath.Join(dir, e.Name()),
		})
	}

	return tools, nil
}

// ParseToolManifest reads a tool.yaml file.
func ParseToolManifest(path string) (*ToolManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var m ToolManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse tool manifest YAML: %w", err)
	}

	return &m, nil
}

// CustomTool wraps a ToolManifest as an executable tool.
type CustomTool struct {
	Manifest ToolManifest
	Dir      string // directory containing the tool
}

// Call executes the custom tool by running its command.
// If Manifest.Stdin is "json", the input JSON is piped to stdin.
// Otherwise, {{param}} placeholders in the command are substituted with input values.
func (t *CustomTool) Call(ctx context.Context, input json.RawMessage) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	var params map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("failed to parse input JSON: %w", err)
		}
	}

	cmdStr := t.Manifest.Command
	if t.Manifest.Stdin != "json" {
		// Substitute {{param}} placeholders.
		for key, val := range params {
			placeholder := "{{" + key + "}}"
			cmdStr = strings.ReplaceAll(cmdStr, placeholder, fmt.Sprintf("%v", val))
		}
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = t.Dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if t.Manifest.Stdin == "json" {
		cmd.Stdin = bytes.NewReader(input)
	}

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("tool %s timed out after %s", t.Manifest.Name, DefaultTimeout)
		}
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("tool %s failed: %s", t.Manifest.Name, errMsg)
	}

	return stdout.String(), nil
}
