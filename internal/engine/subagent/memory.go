package subagent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// --- Memory Scopes ---

// MemoryScope identifies one of three per-type memory storage locations.
type MemoryScope string

const (
	// MemoryScopeUser lives under the user home directory and applies across projects.
	MemoryScopeUser MemoryScope = "user"
	// MemoryScopeProject lives under the project root and is committed to the repo.
	MemoryScopeProject MemoryScope = "project"
	// MemoryScopeLocal lives under the project root but is gitignored and manually edited.
	MemoryScopeLocal MemoryScope = "local"
)

// Operation selects the write behavior for WriteAgentMemoryScope.
type Operation string

const (
	// OperationAppend adds a timestamped entry to the end of the scope file.
	OperationAppend Operation = "append"
	// OperationReplace overwrites the scope file atomically.
	OperationReplace Operation = "replace"
)

// MemoryScopeSizeCap bounds each scope file to 50 KB; oldest entries are
// truncated when an append would exceed the cap.
const MemoryScopeSizeCap = 50 * 1024

// defaultAgentType is used when an agent has no explicit type identifier.
const defaultAgentType = "default"

// --- Blocks ---

// MemoryBlock is one scope's contents, ready for prompt injection.
type MemoryBlock struct {
	Scope   MemoryScope
	Content string
}

// --- Public API ---

// LoadAgentMemory reads all three scopes for agentType and returns any blocks
// that are non-empty. Missing files are silently skipped. Blocks are returned
// in fixed precedence order: user, project, local.
func LoadAgentMemory(agentType, projectRoot string) []MemoryBlock {
	agentType = normalizeAgentType(agentType)

	var blocks []MemoryBlock
	order := []MemoryScope{MemoryScopeUser, MemoryScopeProject, MemoryScopeLocal}

	for _, scope := range order {
		path, err := scopePath(scope, agentType, projectRoot)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		blocks = append(blocks, MemoryBlock{Scope: scope, Content: content})
	}

	return blocks
}

// RenderMemoryBlocks formats memory blocks as agent-memory XML segments for
// injection into a system prompt. Returns an empty string when no blocks are present.
func RenderMemoryBlocks(blocks []MemoryBlock) string {
	if len(blocks) == 0 {
		return ""
	}
	var b strings.Builder
	for i, block := range blocks {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "<agent-memory scope=\"%s\">\n%s\n</agent-memory>", block.Scope, block.Content)
	}
	return b.String()
}

// WriteAgentMemoryScope persists content to the named scope. The local scope
// is read-only from the subagent perspective and is rejected with an error.
// Append adds a timestamp header; replace overwrites atomically. Files exceeding
// MemoryScopeSizeCap are truncated from the oldest entries.
func WriteAgentMemoryScope(scope MemoryScope, agentType, projectRoot, content string, op Operation) error {
	if scope == MemoryScopeLocal {
		return fmt.Errorf("local scope is read-only from subagents: edit the file manually")
	}
	if scope != MemoryScopeUser && scope != MemoryScopeProject {
		return fmt.Errorf("unknown memory scope: %q", string(scope))
	}
	if op == "" {
		op = OperationAppend
	}
	if op != OperationAppend && op != OperationReplace {
		return fmt.Errorf("unknown operation: %q", string(op))
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("content is empty")
	}

	agentType = normalizeAgentType(agentType)
	path, err := scopePath(scope, agentType, projectRoot)
	if err != nil {
		return fmt.Errorf("resolve scope path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create memory directory: %w", err)
	}

	switch op {
	case OperationReplace:
		return replaceAtomic(path, content)
	case OperationAppend:
		return appendEntry(path, content)
	}
	return fmt.Errorf("unreachable operation branch")
}

// InjectAgentMemory augments the given system prompt with memory blocks for the
// agent type. When no blocks exist, the prompt is returned unchanged.
func InjectAgentMemory(systemPrompt, agentType, projectRoot string) string {
	blocks := LoadAgentMemory(agentType, projectRoot)
	rendered := RenderMemoryBlocks(blocks)
	if rendered == "" {
		return systemPrompt
	}
	if systemPrompt == "" {
		return rendered
	}
	return systemPrompt + "\n\n" + rendered
}

// --- Internal ---

func normalizeAgentType(agentType string) string {
	agentType = strings.TrimSpace(agentType)
	if agentType == "" {
		return defaultAgentType
	}
	// Keep a conservative allowlist to prevent path traversal via the type field.
	safe := make([]rune, 0, len(agentType))
	for _, r := range agentType {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			safe = append(safe, r)
		}
	}
	if len(safe) == 0 {
		return defaultAgentType
	}
	return string(safe)
}

func scopePath(scope MemoryScope, agentType, projectRoot string) (string, error) {
	switch scope {
	case MemoryScopeUser:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home: %w", err)
		}
		return filepath.Join(home, ".providence", "agent-memory", agentType, "user.md"), nil
	case MemoryScopeProject:
		if projectRoot == "" {
			return "", fmt.Errorf("project root is empty for project scope")
		}
		return filepath.Join(projectRoot, ".providence", "agent-memory", agentType, "project.md"), nil
	case MemoryScopeLocal:
		if projectRoot == "" {
			return "", fmt.Errorf("project root is empty for local scope")
		}
		return filepath.Join(projectRoot, ".providence", "agent-memory", agentType, "local.md"), nil
	default:
		return "", fmt.Errorf("unknown memory scope: %q", string(scope))
	}
}

func replaceAtomic(path, content string) error {
	trimmed := content
	if len(trimmed) > MemoryScopeSizeCap {
		// Replace should never exceed the cap; keep the newest tail.
		trimmed = trimmed[len(trimmed)-MemoryScopeSizeCap:]
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".memory-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = os.Remove(tmpPath)
	}

	if _, err := tmp.WriteString(trimmed); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

func appendEntry(path, content string) error {
	entry := formatAppendEntry(time.Now().UTC(), content)

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read existing memory: %w", err)
	}

	combined := string(existing)
	if combined != "" && !strings.HasSuffix(combined, "\n") {
		combined += "\n"
	}
	combined += entry

	// Enforce the 50 KB cap by dropping the oldest entries until we fit.
	combined = truncateOldestEntries(combined, MemoryScopeSizeCap)

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open memory file: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	if _, err := f.WriteString(combined); err != nil {
		return fmt.Errorf("write memory file: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync memory file: %w", err)
	}
	return nil
}

// formatAppendEntry returns a canonical entry with a timestamp header.
// Format: "## <RFC3339>\n<content>\n"
func formatAppendEntry(ts time.Time, content string) string {
	return fmt.Sprintf("## %s\n%s\n", ts.Format(time.RFC3339), strings.TrimRight(content, "\n"))
}

// truncateOldestEntries keeps dropping the oldest "## " prefixed section until
// the combined byte length is at most cap. If a single entry exceeds the cap,
// its tail is kept so the newest content always survives.
func truncateOldestEntries(combined string, cap int) string {
	if len(combined) <= cap {
		return combined
	}

	for len(combined) > cap {
		// Find the start of the next entry after the current first one.
		// Entries are delimited by a leading "## " at the start of a line.
		first := strings.Index(combined, "## ")
		if first == -1 {
			// No entry headers found, fall back to raw tail.
			return combined[len(combined)-cap:]
		}

		next := strings.Index(combined[first+1:], "\n## ")
		if next == -1 {
			// Only one entry left and it still exceeds cap; keep the newest tail.
			if len(combined) > cap {
				return combined[len(combined)-cap:]
			}
			return combined
		}

		// Drop everything up to and including the newline before the next entry header.
		combined = combined[first+1+next+1:]
	}
	return combined
}
