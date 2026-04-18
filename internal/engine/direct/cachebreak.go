package direct

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gravitrone/providence-core/internal/engine"
	"github.com/gravitrone/providence-core/internal/engine/direct/tools"
)

// CacheFingerprint captures every input to the API request whose change
// would invalidate the Anthropic prompt cache. A per-turn diff lets
// users see which field flipped when their cache read drops to zero.
type CacheFingerprint struct {
	Model            string
	SystemHashes     []string          // one hash per SystemBlock, in order
	ToolHashes       map[string]string // toolName -> hash of its name + description + schema
	ToolSchemaHashes map[string]string // toolName -> sha256 of its marshaled input schema
}

// fingerprintFromInputs builds a CacheFingerprint from the structured
// blocks and the registered tools. Both sources are the same inputs
// that assemble the Anthropic MessageNewParams request, so any change
// here correlates with a real cache key change.
func fingerprintFromInputs(model string, blocks []engine.SystemBlock, registry *tools.Registry) CacheFingerprint {
	fp := CacheFingerprint{
		Model:            model,
		SystemHashes:     make([]string, len(blocks)),
		ToolHashes:       map[string]string{},
		ToolSchemaHashes: map[string]string{},
	}
	for i, b := range blocks {
		fp.SystemHashes[i] = hash64(b.Text)
	}
	if registry != nil {
		for _, t := range registry.All() {
			schemaHash := hashSchema(t.InputSchema())
			fp.ToolHashes[t.Name()] = hash64(t.Name() + "\x00" + t.Description() + "\x00" + schemaHash)
			fp.ToolSchemaHashes[t.Name()] = schemaHash
		}
	}
	return fp
}

func hashSchema(schema map[string]any) string {
	schemaBytes, _ := json.Marshal(schema)
	sum := sha256.Sum256(schemaBytes)
	return fmt.Sprintf("%x", sum)
}

// hash64 returns a stable 16-hex-char fnv64a hash of the input. fnv is
// plenty for cache-break diagnostics; we just need change detection,
// not cryptographic strength.
func hash64(s string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	out := h.Sum(nil)
	return hex.EncodeToString(out)
}

// DiffCacheFingerprints returns a list of human-readable change lines
// between two fingerprints. Empty slice means the cache prefix is
// stable. Order: Model first, then per-block changes in index order,
// then per-tool changes in alphabetical order.
func DiffCacheFingerprints(prev, next CacheFingerprint) []string {
	if !hasAnyField(prev) {
		// First call - no baseline to diff against.
		return nil
	}

	var lines []string
	if prev.Model != next.Model {
		lines = append(lines, fmt.Sprintf("model: %q -> %q", prev.Model, next.Model))
	}

	prevLen := len(prev.SystemHashes)
	nextLen := len(next.SystemHashes)
	maxLen := prevLen
	if nextLen > maxLen {
		maxLen = nextLen
	}
	for i := 0; i < maxLen; i++ {
		switch {
		case i >= prevLen:
			lines = append(lines, fmt.Sprintf("system_block[%d]: added (%s)", i, next.SystemHashes[i]))
		case i >= nextLen:
			lines = append(lines, fmt.Sprintf("system_block[%d]: removed (was %s)", i, prev.SystemHashes[i]))
		case prev.SystemHashes[i] != next.SystemHashes[i]:
			lines = append(lines, fmt.Sprintf("system_block[%d]: %s -> %s", i, prev.SystemHashes[i], next.SystemHashes[i]))
		}
	}

	// Tool diffs in a deterministic order so repeated runs produce the
	// same diff body byte-for-byte.
	toolNames := map[string]bool{}
	for name := range prev.ToolHashes {
		toolNames[name] = true
	}
	for name := range prev.ToolSchemaHashes {
		toolNames[name] = true
	}
	for name := range next.ToolHashes {
		toolNames[name] = true
	}
	for name := range next.ToolSchemaHashes {
		toolNames[name] = true
	}
	sortedNames := make([]string, 0, len(toolNames))
	for name := range toolNames {
		sortedNames = append(sortedNames, name)
	}
	sort.Strings(sortedNames)
	for _, name := range sortedNames {
		prevHash, prevOK := prev.ToolHashes[name]
		nextHash, nextOK := next.ToolHashes[name]
		prevSchemaHash, prevSchemaOK := prev.ToolSchemaHashes[name]
		nextSchemaHash, nextSchemaOK := next.ToolSchemaHashes[name]
		prevSeen := prevOK || prevSchemaOK
		nextSeen := nextOK || nextSchemaOK
		switch {
		case !prevSeen:
			lines = append(lines, fmt.Sprintf("cachebreak: tool '%s' added", name))
		case !nextSeen:
			lines = append(lines, fmt.Sprintf("cachebreak: tool '%s' removed", name))
		case prevSchemaOK && nextSchemaOK && prevSchemaHash != nextSchemaHash:
			lines = append(lines, fmt.Sprintf("cachebreak: tool '%s' schema changed", name))
		case prevOK && nextOK && prevHash != nextHash:
			lines = append(lines, fmt.Sprintf("cachebreak: tool '%s' description changed", name))
		}
	}
	return lines
}

// hasAnyField returns true once a fingerprint has been populated at
// least once. Used to suppress the "first call diff" from being written
// as a spurious everything-changed event.
func hasAnyField(fp CacheFingerprint) bool {
	return fp.Model != "" || len(fp.SystemHashes) > 0 || len(fp.ToolHashes) > 0 || len(fp.ToolSchemaHashes) > 0
}

// cacheBreakDir holds the directory where diff files are written.
// Mutable so tests can redirect without racing on os.UserHomeDir.
var (
	cacheBreakDirOnce sync.Once
	cacheBreakDir     string
	cacheBreakDirMu   sync.RWMutex
)

// SetCacheBreakDir overrides the output directory (for tests).
func SetCacheBreakDir(dir string) {
	cacheBreakDirMu.Lock()
	defer cacheBreakDirMu.Unlock()
	cacheBreakDir = dir
}

// defaultCacheBreakDir resolves ~/.providence/cache-breaks/ lazily.
func defaultCacheBreakDir() string {
	cacheBreakDirOnce.Do(func() {
		cacheBreakDirMu.Lock()
		defer cacheBreakDirMu.Unlock()
		if cacheBreakDir != "" {
			return
		}
		home, err := os.UserHomeDir()
		if err != nil {
			cacheBreakDir = filepath.Join(os.TempDir(), "providence-cache-breaks")
			return
		}
		cacheBreakDir = filepath.Join(home, ".providence", "cache-breaks")
	})
	cacheBreakDirMu.RLock()
	defer cacheBreakDirMu.RUnlock()
	return cacheBreakDir
}

// WriteCacheBreakDiff persists a diff to a timestamped file and returns
// the file path. Callers should ignore the path if they just want
// fire-and-forget behaviour; returning it keeps the function testable.
func WriteCacheBreakDiff(lines []string, now time.Time) (string, error) {
	if len(lines) == 0 {
		return "", nil
	}
	dir := defaultCacheBreakDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create cache-break dir: %w", err)
	}
	stamp := now.UTC().Format("20060102-150405")
	tag := hash64(strings.Join(lines, "\n"))[:6]
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.diff", stamp, tag))
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("write cache-break diff: %w", err)
	}
	return path, nil
}

// checkAndRecordCacheBreak is the hook called from the engine hot
// path. It computes the current fingerprint, diffs it against the
// stored last fingerprint, writes a diff file if anything changed,
// and updates the stored value. Errors are swallowed - diagnostics
// must never fail a real request.
func (e *DirectEngine) checkAndRecordCacheBreak() {
	e.cacheFpMu.Lock()
	defer e.cacheFpMu.Unlock()

	current := fingerprintFromInputs(e.model, e.blocks, e.registry)
	diff := DiffCacheFingerprints(e.lastFingerprint, current)
	if len(diff) > 0 {
		_, _ = WriteCacheBreakDiff(diff, time.Now())
	}
	e.lastFingerprint = current
}
