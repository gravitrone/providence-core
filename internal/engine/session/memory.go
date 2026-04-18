package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// --- Constants ---

// MemoryStaleAfter is how long a session memory file may sit on disk before it
// is considered stale. Stale files are ignored at read time because they are
// almost certainly crash survivors from a dead session.
const MemoryStaleAfter = 7 * 24 * time.Hour

// DefaultMemoryTurnInterval is the default number of completed turns between
// memory writes when the engine has not supplied an override.
const DefaultMemoryTurnInterval = 5

// MemorySummarizationPrompt is the fixed instruction handed to the fork
// subagent that summarizes the last N turns into a session memory file.
// Kept as a constant so callers can tweak it without chasing string literals.
const MemorySummarizationPrompt = `Summarize the current user goals, decisions made, constraints mentioned, and open questions from the last N turns of conversation you have access to. Output markdown, under 500 words. Preserve technical specifics like file paths, function signatures, flag names, and API names verbatim. Prefer concrete quoted snippets over paraphrase. Do not speculate beyond what the transcript states.`

// --- Errors ---

// ErrMemoryStale is returned by ReadSessionMemory when the file exists but its
// mtime is older than MemoryStaleAfter. Callers should log a warning and treat
// the read as a miss.
var ErrMemoryStale = errors.New("session memory file is stale")

// ErrEmptySessionID is returned when the caller passes a blank session id. We
// refuse to touch disk for an empty id because collisions would be guaranteed.
var ErrEmptySessionID = errors.New("session id is empty")

// --- Paths ---

// memoryDirOverride is set by tests to redirect storage off the user home dir.
// Guarded by memoryDirMu so concurrent readers / test cleanups don't race.
var (
	memoryDirOverride string
	memoryDirMu       sync.RWMutex
)

// SetMemoryDirForTesting overrides the on-disk memory directory. Intended for
// tests only; production callers should leave the default root.
func SetMemoryDirForTesting(dir string) {
	memoryDirMu.Lock()
	defer memoryDirMu.Unlock()
	memoryDirOverride = dir
}

// MemoryDir returns the directory where session memory files live. The default
// is ~/.providence/session-memory. Tests may redirect this via the override.
func MemoryDir() string {
	memoryDirMu.RLock()
	override := memoryDirOverride
	memoryDirMu.RUnlock()

	if override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// Fall back to the current working directory so we never panic.
		return filepath.Join(".providence", "session-memory")
	}
	return filepath.Join(home, ".providence", "session-memory")
}

// MemoryPath returns the absolute path where the memory for sessionID lives.
// The caller is responsible for passing a non-empty session id; this function
// does not validate because callers frequently use it for display.
func MemoryPath(sessionID string) string {
	return filepath.Join(MemoryDir(), sanitizeSessionID(sessionID)+".md")
}

// --- Writer ---

// WriteSessionMemory atomically writes content to the memory file for
// sessionID. The write is tempfile + rename so a crash mid-write cannot leave
// a truncated file in place. An empty sessionID is rejected; an empty content
// string is permitted and produces an empty file (useful for clearing).
func WriteSessionMemory(sessionID, content string) error {
	if strings.TrimSpace(sessionID) == "" {
		return ErrEmptySessionID
	}

	dir := MemoryDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create session memory dir: %w", err)
	}

	final := MemoryPath(sessionID)

	// Tempfile in the same directory so rename is atomic on the same fs.
	tmp, err := os.CreateTemp(dir, ".memory-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp memory file: %w", err)
	}
	tmpName := tmp.Name()

	// Best-effort cleanup if something below us fails partway.
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp memory file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp memory file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp memory file: %w", err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		cleanup()
		return fmt.Errorf("rename temp memory file: %w", err)
	}
	return nil
}

// --- Reader ---

// ReadSessionMemory returns the stored memory for sessionID. If no file exists
// it returns ("", nil) so callers can treat "no memory yet" as a non-error
// miss. If the file exists but its mtime is older than MemoryStaleAfter, it
// returns ("", ErrMemoryStale) so the caller can log and fall back cleanly.
func ReadSessionMemory(sessionID string) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return "", ErrEmptySessionID
	}

	path := MemoryPath(sessionID)
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("stat session memory: %w", err)
	}

	if time.Since(info.ModTime()) > MemoryStaleAfter {
		return "", ErrMemoryStale
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read session memory: %w", err)
	}
	return string(data), nil
}

// --- Helpers ---

// sanitizeSessionID strips path separators so a malicious or malformed session
// id cannot escape MemoryDir. We intentionally keep the result human-readable
// so operators can inspect the directory with ls.
func sanitizeSessionID(sessionID string) string {
	cleaned := strings.ReplaceAll(sessionID, string(os.PathSeparator), "_")
	cleaned = strings.ReplaceAll(cleaned, "/", "_")
	cleaned = strings.ReplaceAll(cleaned, "\\", "_")
	cleaned = strings.ReplaceAll(cleaned, "..", "_")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		cleaned = "unknown"
	}
	return cleaned
}
