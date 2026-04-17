package tools

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// SecurityCheck is the result of a bash command security check.
type SecurityCheck struct {
	Allowed bool
	Reason  string
}

// zshDangerousCommands are zsh builtins that can bypass security checks.
// Ported from CC's bashSecurity.ts ZSH_DANGEROUS_COMMANDS set.
var zshDangerousCommands = map[string]bool{
	"zmodload": true, // gateway to dangerous zsh modules
	"emulate":  true, // emulate -c is eval-equivalent
	"sysopen":  true, // zsh/system: fine-grained file open
	"sysread":  true, // zsh/system: fd reads
	"syswrite": true, // zsh/system: fd writes
	"sysseek":  true, // zsh/system: fd seeks
	"zpty":     true, // pseudo-terminal execution
	"ztcp":     true, // TCP connections for exfil
	"zsocket":  true, // Unix/TCP sockets
	"zf_rm":    true, // builtin rm from zsh/files
	"zf_mv":    true, // builtin mv from zsh/files
	"zf_ln":    true, // builtin ln from zsh/files
	"zf_chmod": true, // builtin chmod from zsh/files
	"zf_chown": true, // builtin chown from zsh/files
	"zf_mkdir": true, // builtin mkdir from zsh/files
	"zf_rmdir": true, // builtin rmdir from zsh/files
	"zf_chgrp": true, // builtin chgrp from zsh/files
}

// commandSubstitutionPatterns detect shell expansion/substitution constructs.
var commandSubstitutionPatterns = []struct {
	pattern *regexp.Regexp
	message string
}{
	{regexp.MustCompile(`<\(`), "process substitution <()"},
	{regexp.MustCompile(`>\(`), "process substitution >()"},
	{regexp.MustCompile(`=\(`), "zsh process substitution =()"},
	{regexp.MustCompile(`(?:^|[\s;&|])=[a-zA-Z_]`), "zsh equals expansion (=cmd)"},
}

// destructivePatterns match commands that can cause irreversible damage.
var destructivePatterns = []struct {
	pattern *regexp.Regexp
	message string
}{
	{regexp.MustCompile(`rm\s+(-[a-zA-Z]*f[a-zA-Z]*\s+)?/\s*$`), "rm at filesystem root"},
	{regexp.MustCompile(`rm\s+-[a-zA-Z]*r[a-zA-Z]*f[a-zA-Z]*\s+/\s*$`), "rm -rf at root"},
	{regexp.MustCompile(`rm\s+-[a-zA-Z]*f[a-zA-Z]*r[a-zA-Z]*\s+/\s*$`), "rm -fr at root"},
	{regexp.MustCompile(`mkfs\b`), "filesystem format command"},
	{regexp.MustCompile(`dd\s+.*of=/dev/[sh]d`), "dd to block device"},
	{regexp.MustCompile(`>\s*/dev/[sh]d`), "redirect to block device"},
}

// devAccessPattern detects /dev/ device access in commands.
var devAccessPattern = regexp.MustCompile(`/dev/(?:sd|hd|nvme|vd|xvd|dm-|loop|sr|fd|random|urandom|zero|mem|kmem|port)`)

// procEnvironPattern detects access to /proc/*/environ.
var procEnvironPattern = regexp.MustCompile(`/proc/\d+/environ|/proc/self/environ`)

// sedBacktickPattern detects backtick injection in sed expressions.
// A backtick in a sed expression can execute commands if not properly escaped.
var sedBacktickPattern = regexp.MustCompile("sed\\s.*`")

// controlCharPattern detects control characters that can manipulate terminal behavior.
var controlCharPattern = regexp.MustCompile(`[\x00-\x08\x0e-\x1f\x7f]`)

// CheckBashSecurity validates a command against security rules.
// Returns SecurityCheck with Allowed=true if the command passes all checks.
func CheckBashSecurity(command string) SecurityCheck {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return SecurityCheck{Allowed: true, Reason: "empty command"}
	}

	// Check for control characters.
	if controlCharPattern.MatchString(command) {
		return SecurityCheck{
			Allowed: false,
			Reason:  "command contains control characters that could manipulate terminal behavior",
		}
	}

	if containsInPlaceEdit(command) {
		return SecurityCheck{
			Allowed: false,
			Reason:  fmt.Errorf("bash: in-place edit (sed -i) not allowed; use Edit or Write tools").Error(),
		}
	}

	// Extract base command (first word of each pipeline segment).
	segments := splitCommandSegments(command)
	for _, seg := range segments {
		base := extractBaseCommand(seg)

		// Block dangerous zsh builtins.
		if zshDangerousCommands[base] {
			return SecurityCheck{
				Allowed: false,
				Reason:  "blocked dangerous zsh builtin: " + base,
			}
		}

		// Block eval with potential injection patterns.
		if base == "eval" {
			return SecurityCheck{
				Allowed: false,
				Reason:  "eval is blocked - use direct commands instead",
			}
		}
	}

	// Check for /dev/ device access.
	if devAccessPattern.MatchString(command) {
		return SecurityCheck{
			Allowed: false,
			Reason:  "blocked access to device files in /dev/",
		}
	}

	// Check for /proc/*/environ access.
	if procEnvironPattern.MatchString(command) {
		return SecurityCheck{
			Allowed: false,
			Reason:  "blocked access to process environment (/proc/*/environ)",
		}
	}

	// Check destructive patterns.
	for _, dp := range destructivePatterns {
		if dp.pattern.MatchString(command) {
			return SecurityCheck{
				Allowed: false,
				Reason:  "blocked destructive command: " + dp.message,
			}
		}
	}

	// Check for sed backtick injection.
	if sedBacktickPattern.MatchString(command) {
		return SecurityCheck{
			Allowed: false,
			Reason:  "blocked potential sed backtick injection - backticks in sed expressions can execute commands",
		}
	}

	return SecurityCheck{Allowed: true, Reason: "passed all checks"}
}

// containsInPlaceEdit returns true when a command segment invokes sed, perl,
// or awk with an in-place edit flag.
func containsInPlaceEdit(cmd string) bool {
	segments := splitCommandSegments(cmd)
	for _, segment := range segments {
		tokens := tokenizeCommand(segment)
		commandIdx := commandTokenIndex(tokens)
		if commandIdx == -1 {
			continue
		}

		base := filepath.Base(tokens[commandIdx])
		if !isInPlaceEditCommand(base) {
			continue
		}

		if segmentContainsInPlaceEdit(base, tokens[commandIdx+1:]) {
			return true
		}
	}

	return false
}

// splitCommandSegments splits a command string on shell operators (;, &&, ||, |).
// This is a simplified split - does not handle quoted strings perfectly but
// is sufficient for base command extraction.
func splitCommandSegments(cmd string) []string {
	var segments []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]

		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}

		if ch == '\\' && !inSingle {
			escaped = true
			current.WriteByte(ch)
			continue
		}

		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			current.WriteByte(ch)
			continue
		}

		if ch == '"' && !inSingle {
			inDouble = !inDouble
			current.WriteByte(ch)
			continue
		}

		if !inSingle && !inDouble {
			// Check for operators
			if ch == ';' || ch == '|' {
				seg := strings.TrimSpace(current.String())
				if seg != "" {
					segments = append(segments, seg)
				}
				current.Reset()
				// Skip double operators (&&, ||)
				if i+1 < len(cmd) && (cmd[i+1] == '&' || cmd[i+1] == '|') {
					i++
				}
				continue
			}
			if ch == '&' {
				if i+1 < len(cmd) && cmd[i+1] == '&' {
					seg := strings.TrimSpace(current.String())
					if seg != "" {
						segments = append(segments, seg)
					}
					current.Reset()
					i++ // skip second &
					continue
				}
				// Single & (background) - still split
				seg := strings.TrimSpace(current.String())
				if seg != "" {
					segments = append(segments, seg)
				}
				current.Reset()
				continue
			}
		}

		current.WriteByte(ch)
	}

	seg := strings.TrimSpace(current.String())
	if seg != "" {
		segments = append(segments, seg)
	}

	return segments
}

// extractBaseCommand returns the first word of a command string,
// stripping any leading env vars (VAR=val) and whitespace.
func extractBaseCommand(segment string) string {
	trimmed := strings.TrimSpace(segment)
	// Skip env variable assignments at the start (VAR=val cmd args)
	for {
		// Check for VAR=something pattern
		eqIdx := strings.Index(trimmed, "=")
		spIdx := strings.Index(trimmed, " ")
		if eqIdx > 0 && (spIdx == -1 || eqIdx < spIdx) {
			// There's an = before the first space, this is a var assignment
			if spIdx == -1 {
				return trimmed // Just an assignment, no command
			}
			trimmed = strings.TrimSpace(trimmed[spIdx+1:])
			continue
		}
		break
	}

	// First word is the command
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// tokenizeCommand splits a shell command into whitespace-delimited tokens while
// preserving empty quoted arguments, including empty string arguments.
func tokenizeCommand(cmd string) []string {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	started := false

	flush := func() {
		if !started {
			return
		}
		tokens = append(tokens, current.String())
		current.Reset()
		started = false
	}

	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]

		if escaped {
			current.WriteByte(ch)
			escaped = false
			started = true
			continue
		}

		if ch == '\\' && !inSingle {
			escaped = true
			started = true
			continue
		}

		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			started = true
			continue
		}

		if ch == '"' && !inSingle {
			inDouble = !inDouble
			started = true
			continue
		}

		if !inSingle && !inDouble && (ch == ' ' || ch == '\t' || ch == '\n') {
			flush()
			continue
		}

		current.WriteByte(ch)
		started = true
	}

	flush()
	return tokens
}

// commandTokenIndex returns the first non-env-assignment token in a segment.
func commandTokenIndex(tokens []string) int {
	for i, token := range tokens {
		if token == "" {
			continue
		}
		if !isEnvAssignment(token) {
			return i
		}
	}

	return -1
}

// isEnvAssignment returns true when a token is a simple leading env assignment.
func isEnvAssignment(token string) bool {
	eqIdx := strings.Index(token, "=")
	return eqIdx > 0
}

// isInPlaceEditCommand returns true for commands that support in-place edits.
func isInPlaceEditCommand(command string) bool {
	switch command {
	case "sed", "perl", "awk":
		return true
	default:
		return false
	}
}

// segmentContainsInPlaceEdit returns true when the command arguments request
// in-place editing for the given command.
func segmentContainsInPlaceEdit(command string, args []string) bool {
	for i, arg := range args {
		switch command {
		case "sed", "perl":
			if arg == "-i" || arg == "--in-place" {
				return true
			}
			if strings.HasPrefix(arg, "-i") || strings.HasPrefix(arg, "--in-place=") {
				return true
			}
		case "awk":
			if arg == "-i" && i+1 < len(args) && args[i+1] == "inplace" {
				return true
			}
		}
	}

	return false
}
