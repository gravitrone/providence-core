package customtools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// placeholderPattern matches a {{name}} placeholder. Used both to detect
// pure-placeholder tokens in the command template and to reject embedded
// placeholders inside larger shell tokens (e.g. "prefix{{x}}suffix").
var placeholderPattern = regexp.MustCompile(`\{\{([a-zA-Z_][a-zA-Z0-9_]*)\}\}`)

// purePlaceholderPattern matches a token that is exactly a single
// placeholder with no surrounding text. These are the only safe positional
// substitutions.
var purePlaceholderPattern = regexp.MustCompile(`^\{\{([a-zA-Z_][a-zA-Z0-9_]*)\}\}$`)

// buildArgv parses the command template into an argv slice, substituting
// placeholders with input values as separate positional arguments (never
// concatenated into a shell string). Tokens that contain a placeholder
// embedded in larger text are rejected with a clear error because they
// cannot be resolved safely without a shell.
//
// Example:
//
//	command: "grep {{pattern}} {{file}}"
//	params:  {"pattern": "; rm -rf ~", "file": "a.txt"}
//	argv:    ["grep", "; rm -rf ~", "a.txt"]
//
// The dangerous value survives as a single argv element passed to grep,
// never as shell-interpreted text.
func buildArgv(cmdTemplate string, params map[string]any) ([]string, error) {
	tokens, err := splitShellTokens(cmdTemplate)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty command template")
	}

	argv := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if m := purePlaceholderPattern.FindStringSubmatch(tok); m != nil {
			key := m[1]
			val, ok := params[key]
			if !ok {
				return nil, fmt.Errorf("missing input for placeholder %q", key)
			}
			argv = append(argv, fmt.Sprintf("%v", val))
			continue
		}
		// reject placeholders embedded inside a larger token. "{{x}}-suffix"
		// cannot become a single safe argv element without shell semantics,
		// so we refuse rather than silently concatenate and reintroduce the
		// injection primitive.
		if placeholderPattern.MatchString(tok) {
			return nil, fmt.Errorf(
				"placeholder embedded inside token %q is not supported; "+
					"use a whole-token placeholder like \"{{name}}\" or pass the value via the $P_* env convention",
				tok,
			)
		}
		argv = append(argv, tok)
	}
	return argv, nil
}

// splitShellTokens splits a command template on whitespace while respecting
// single and double quotes. Quotes are stripped from the resulting tokens.
// Backslash escaping is intentionally not supported so the grammar stays
// small and predictable; tool authors needing complex values should pass
// them via inputs (which are safely substituted as whole-token placeholders)
// or via the $P_* env convention.
func splitShellTokens(s string) ([]string, error) {
	var tokens []string
	var cur strings.Builder
	inSingle := false
	inDouble := false
	hasContent := false

	flush := func() {
		if hasContent || cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
			hasContent = false
		}
	}

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			hasContent = true
		case c == '"' && !inSingle:
			inDouble = !inDouble
			hasContent = true
		case (c == ' ' || c == '\t') && !inSingle && !inDouble:
			flush()
		default:
			cur.WriteByte(c)
			hasContent = true
		}
	}
	if inSingle || inDouble {
		return nil, fmt.Errorf("unterminated quote in command template")
	}
	flush()
	return tokens, nil
}

// envFromParams returns "P_<UPPER_KEY>=<value>" strings suitable for
// cmd.Env, letting tool scripts reference inputs via $P_NAME without shell
// interpolation at substitution time.
func envFromParams(params map[string]any) []string {
	if len(params) == 0 {
		return nil
	}
	env := make([]string, 0, len(params))
	for k, v := range params {
		env = append(env, fmt.Sprintf("P_%s=%v", strings.ToUpper(k), v))
	}
	return env
}

// Call executes the custom tool by running its command.
// If Manifest.Stdin is "json", the input JSON is piped to stdin and the
// command template is executed verbatim (still tokenized, no shell).
// Otherwise, {{param}} placeholders are substituted as positional argv
// elements; placeholder values are never concatenated into a shell string.
// Input values are also exposed as $P_<UPPER_KEY> env vars for tools that
// prefer the env convention.
func (t *CustomTool) Call(ctx context.Context, input json.RawMessage) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	var params map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("failed to parse input JSON: %w", err)
		}
	}

	argv, err := buildArgv(t.Manifest.Command, params)
	if err != nil {
		return "", fmt.Errorf("tool %s: invalid command template: %w", t.Manifest.Name, err)
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = t.Dir
	if envExtra := envFromParams(params); len(envExtra) > 0 {
		cmd.Env = append(os.Environ(), envExtra...)
	}

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
