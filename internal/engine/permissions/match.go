package permissions

import (
	"path/filepath"
	"strings"
)

// --- Safety Data ---

// safetyPaths are directories and files that always require user approval,
// even in bypass mode. Modifications to these can compromise the system.
var safetyPaths = []string{
	".git/",
	".git\\",
	".claude/",
	".claude\\",
	".bashrc",
	".bash_profile",
	".zshrc",
	".zprofile",
	".profile",
	".zshenv",
	".config/fish/",
}

// fileEditTools are tools that modify files on disk.
var fileEditTools = map[string]bool{
	"Write": true,
	"Edit":  true,
}

// readOnlyTools are tools that only read state without side effects.
var readOnlyTools = map[string]bool{
	"Read": true,
	"Glob": true,
	"Grep": true,
}

// --- Rule Matching ---

// matchesRules checks if toolName+input matches any rule in the list.
func matchesRules(rules []Rule, toolName string, input interface{}) bool {
	for _, rule := range rules {
		if matchPattern(rule.Pattern, toolName, input) {
			return true
		}
	}
	return false
}

// matchPattern matches a single rule pattern against tool+input.
// Supported pattern formats:
//   - "Bash"           -> exact tool name match
//   - "mcp__github__*" -> wildcard tool name match
//   - "Bash(git *)"    -> tool name + argument glob
//   - "Read(/home/*)"  -> tool name + path glob
func matchPattern(pattern string, toolName string, input interface{}) bool {
	// Check for parenthesized argument pattern: "ToolName(argGlob)"
	if parenIdx := strings.IndexByte(pattern, '('); parenIdx >= 0 {
		if !strings.HasSuffix(pattern, ")") {
			return false
		}
		namePattern := pattern[:parenIdx]
		argGlob := pattern[parenIdx+1 : len(pattern)-1]

		// Match the tool name part
		if !matchGlob(namePattern, toolName) {
			return false
		}

		// Match the argument part
		arg := extractArg(input)
		if arg == "" {
			return false
		}
		matched, err := filepath.Match(argGlob, arg)
		if err != nil {
			return false
		}
		return matched
	}

	// Plain tool name match (supports wildcards)
	return matchGlob(pattern, toolName)
}

// matchGlob does filepath.Match-style glob matching.
func matchGlob(pattern, s string) bool {
	matched, err := filepath.Match(pattern, s)
	if err != nil {
		return false
	}
	return matched
}

// extractArg pulls the primary argument string from tool input.
// Supports map[string]interface{} with "command" or "file_path" keys,
// and plain string input.
func extractArg(input interface{}) string {
	switch v := input.(type) {
	case string:
		return v
	case map[string]interface{}:
		// Try command first (Bash tool), then file_path (Read/Write/Edit)
		if cmd, ok := v["command"].(string); ok {
			return cmd
		}
		if fp, ok := v["file_path"].(string); ok {
			return fp
		}
	}
	return ""
}

// --- Safety Checks ---

// isSafetyPath checks if the tool targets a protected path.
func isSafetyPath(toolName string, input interface{}) bool {
	arg := extractArg(input)
	if arg == "" {
		return false
	}

	// For Bash tool, check if the command references safety paths
	if toolName == "Bash" {
		for _, sp := range safetyPaths {
			if strings.Contains(arg, sp) {
				return true
			}
		}
		return false
	}

	// For file tools, check the file path
	for _, sp := range safetyPaths {
		if strings.Contains(arg, sp) {
			return true
		}
	}

	// Check home-relative shell configs
	if isShellConfig(arg) {
		return true
	}

	return false
}

// isShellConfig checks if a path ends with a known shell config filename.
func isShellConfig(path string) bool {
	base := filepath.Base(path)
	shellConfigs := []string{".bashrc", ".bash_profile", ".zshrc", ".zprofile", ".profile", ".zshenv"}
	for _, cfg := range shellConfigs {
		if base == cfg {
			return true
		}
	}
	return false
}

// --- Mode Helpers ---

// isFileEditTool checks if the tool is a file modification tool.
func isFileEditTool(toolName string) bool {
	return fileEditTools[toolName]
}

// isReadOnlyTool checks if the tool is read-only.
func isReadOnlyTool(toolName string) bool {
	return readOnlyTools[toolName]
}

// isAcceptEditsCommand checks if a Bash command starts with an allowed command
// from the AcceptEditsAllowedCommands list.
func isAcceptEditsCommand(toolName string, input interface{}) bool {
	if toolName != "Bash" {
		return false
	}
	arg := extractArg(input)
	if arg == "" {
		return false
	}
	for _, cmd := range AcceptEditsAllowedCommands {
		if arg == cmd || strings.HasPrefix(arg, cmd+" ") {
			return true
		}
	}
	return false
}
