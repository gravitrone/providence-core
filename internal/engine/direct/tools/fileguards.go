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
