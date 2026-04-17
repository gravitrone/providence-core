package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// MaxEditableFileSize caps Edit/Write targets at 100 MiB. Beyond this
// the in-memory read-modify-write pattern wastes RAM and is almost
// always a sign that the assistant is about to do something destructive
// to a binary asset. Tighter than the 1 GiB used by the reference
// implementation, in line with Providence's narrower tool surface.
const MaxEditableFileSize = 100 * 1024 * 1024

var dangerousHomeFiles = []string{
	".bashrc",
	".bash_profile",
	".bash_login",
	".zshrc",
	".zshenv",
	".zprofile",
	".zlogin",
	".profile",
	filepath.Join(".config", "fish", "config.fish"),
	filepath.Join(".config", "nushell", "config.nu"),
	filepath.Join(".docker", "config.json"),
	".netrc",
	".gitconfig",
	".npmrc",
	".pypirc",
	filepath.Join(".kube", "config"),
}

var dangerousHomeDirs = []string{
	".ssh",
	".aws",
	".gcloud",
	filepath.Join("Library", "Application Support", "Google", "Chrome"),
	filepath.Join("Library", "Application Support", "Firefox"),
	filepath.Join("Library", "Keychains"),
}

var dangerousSystemFiles = []string{
	"/etc/sudoers",
	"/etc/passwd",
	"/etc/shadow",
}

var dangerousSystemDirs = []string{
	"/etc/ssh",
}

// SizeGuardError returns a non-nil ToolResult.Content string if path
// exceeds the size cap. Empty string means the target is within
// bounds (or does not exist yet, which is fine for Write-as-create).
func SizeGuardError(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		// Missing files fall through: Write-as-create is allowed.
		return ""
	}
	if info.Size() > MaxEditableFileSize {
		return fmt.Sprintf(
			"file is %d bytes; Edit and Write refuse targets larger than %d bytes (100 MiB)",
			info.Size(), MaxEditableFileSize,
		)
	}
	return ""
}

// IsDangerousPath returns true when path resolves to a shell-init file,
// credential store, browser profile, or system account file that tool
// operations should refuse unless the user explicitly confirms it.
func IsDangerousPath(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}

	absPath := canonicalizeDangerousPath(path)

	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		homePath := canonicalizeDangerousPath(home)
		for _, rel := range dangerousHomeFiles {
			if absPath == canonicalizeDangerousPath(filepath.Join(homePath, rel)) {
				return true
			}
		}
		for _, rel := range dangerousHomeDirs {
			if isDangerousDirMatch(absPath, canonicalizeDangerousPath(filepath.Join(homePath, rel))) {
				return true
			}
		}
	}

	for _, filePath := range dangerousSystemFiles {
		if absPath == canonicalizeDangerousPath(filePath) {
			return true
		}
	}
	for _, dirPath := range dangerousSystemDirs {
		if isDangerousDirMatch(absPath, canonicalizeDangerousPath(dirPath)) {
			return true
		}
	}

	return false
}

// IsSettingsFile returns true if the path looks like Providence or
// Claude Code config that the caller must not corrupt. We match on
// suffixes in both their absolute ("/.providence/...") and
// relative (".providence/...") forms so either path shape is caught.
func IsSettingsFile(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	for _, suffix := range []string{
		".providence/config.toml",
		".providence/permissions.toml",
		".claude/settings.json",
		".claude/settings.local.json",
	} {
		if strings.HasSuffix(clean, "/"+suffix) || clean == suffix {
			return true
		}
	}
	return false
}

// ValidateSettingsContent parses the proposed new content as TOML (for
// .toml files) or JSON (for .json files) and returns an error if the
// parse fails. Returns nil for paths that are not settings files or
// whose format we do not recognise.
func ValidateSettingsContent(path, content string) error {
	if !IsSettingsFile(path) {
		return nil
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	switch {
	case strings.HasSuffix(clean, ".toml"):
		var v map[string]any
		if _, err := toml.Decode(content, &v); err != nil {
			return fmt.Errorf("settings file would not parse as TOML: %w", err)
		}
	case strings.HasSuffix(clean, ".json"):
		// JSON is cheap to validate via the stdlib decoder without
		// importing encoding/json here - callers inside this package
		// can use json.Valid directly.
		if !jsonValid(content) {
			return fmt.Errorf("settings file would not parse as JSON")
		}
	}
	return nil
}

func canonicalizeDangerousPath(path string) string {
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return filepath.Clean(path)
	}
	absPath = filepath.Clean(absPath)

	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		return filepath.Clean(resolved)
	}

	current := absPath
	var unresolved []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			parts := make([]string, 0, len(unresolved)+1)
			parts = append(parts, resolved)
			for i := len(unresolved) - 1; i >= 0; i-- {
				parts = append(parts, unresolved[i])
			}
			return filepath.Clean(filepath.Join(parts...))
		}

		parent := filepath.Dir(current)
		if parent == current {
			return absPath
		}

		unresolved = append(unresolved, filepath.Base(current))
		current = parent
	}
}

func isDangerousDirMatch(path, dangerousDir string) bool {
	return path == dangerousDir || strings.HasPrefix(path, dangerousDir+string(os.PathSeparator))
}
