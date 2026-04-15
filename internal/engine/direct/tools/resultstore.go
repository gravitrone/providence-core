package tools

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SpillThreshold is the size in bytes above which tool output is
// written to disk instead of shipped to the model inline. 25k chars
// keeps most well-bounded tool outputs (test summaries, lint reports,
// git log tails) inline while giving us a spill path for runaway logs.
const SpillThreshold = 25_000

// SpillPreviewBytes controls how many bytes of head + tail are
// returned inline when a spill happens. 2k on each side gives the
// model enough context to cite either end.
const SpillPreviewBytes = 2_000

// spillDir holds the directory where large outputs are written.
// Mutable so tests can redirect without racing on os.UserHomeDir.
var (
	spillDirOnce sync.Once
	spillDir     string
	spillDirMu   sync.RWMutex
)

// SetSpillDir overrides the output directory (for tests).
func SetSpillDir(dir string) {
	spillDirMu.Lock()
	defer spillDirMu.Unlock()
	spillDir = dir
}

// defaultSpillDir resolves ~/.providence/tool-results/ lazily.
func defaultSpillDir() string {
	spillDirOnce.Do(func() {
		spillDirMu.Lock()
		defer spillDirMu.Unlock()
		if spillDir != "" {
			return
		}
		home, err := os.UserHomeDir()
		if err != nil {
			spillDir = filepath.Join(os.TempDir(), "providence-tool-results")
			return
		}
		spillDir = filepath.Join(home, ".providence", "tool-results")
	})
	spillDirMu.RLock()
	defer spillDirMu.RUnlock()
	return spillDir
}

// SpillIfLarge inspects content; if it exceeds SpillThreshold, writes
// the full body to a per-session file and returns a short preview
// containing the head, a marker line with the spill path, and the
// tail. For content under the threshold it returns the input as-is.
//
// The sessionID is used to namespace spill files so parallel sessions
// do not collide. A zero-length sessionID falls back to "session".
func SpillIfLarge(sessionID, toolName, content string) (short string, path string) {
	if len(content) <= SpillThreshold {
		return content, ""
	}

	if sessionID == "" {
		sessionID = "session"
	}
	dir := filepath.Join(defaultSpillDir(), sanitiseForPath(sessionID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		// If we cannot spill, fall back to the untouched content -
		// the caller's existing truncation remains in place.
		return content, ""
	}

	path = filepath.Join(dir, fmt.Sprintf("%d-%s-%s.out",
		time.Now().UnixMilli(), sanitiseForPath(toolName), randTag(4)))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return content, ""
	}

	head := content[:SpillPreviewBytes]
	tail := content[len(content)-SpillPreviewBytes:]

	var b strings.Builder
	b.Grow(SpillPreviewBytes*2 + 256)
	b.WriteString(head)
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("[... %d bytes elided; full output: %s]", len(content)-2*SpillPreviewBytes, path))
	b.WriteString("\n\n")
	b.WriteString(tail)
	return b.String(), path
}

// sanitiseForPath drops characters that would confuse a filesystem
// path. Keeps a-z0-9 dash underscore; anything else collapses to _.
func sanitiseForPath(s string) string {
	if s == "" {
		return "_"
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// randTag returns a hex tag of n bytes (2n chars) for collision
// avoidance when multiple tool results land within the same
// millisecond.
func randTag(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// Fallback: use the unix nano as a deterministic tag.
		return fmt.Sprintf("%x", time.Now().UnixNano()%0xffffffff)
	}
	return hex.EncodeToString(buf)
}
