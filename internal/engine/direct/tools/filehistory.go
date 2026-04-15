package tools

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// FileHistory retention knobs. Snapshots older than this OR beyond
// the count cap on any single path are evicted on the next Snapshot
// call. 20 snapshots or 7 days whichever is tighter.
const (
	historyMaxPerPath = 20
	historyMaxAge     = 7 * 24 * time.Hour
)

// fileHistoryDir holds the directory where gzipped snapshots live.
// Mutable so tests can redirect.
var (
	fileHistoryDirOnce sync.Once
	fileHistoryDir     string
	fileHistoryDirMu   sync.RWMutex
)

// SetFileHistoryDir overrides the snapshot directory (for tests).
func SetFileHistoryDir(dir string) {
	fileHistoryDirMu.Lock()
	defer fileHistoryDirMu.Unlock()
	fileHistoryDir = dir
}

func defaultFileHistoryDir() string {
	fileHistoryDirOnce.Do(func() {
		fileHistoryDirMu.Lock()
		defer fileHistoryDirMu.Unlock()
		if fileHistoryDir != "" {
			return
		}
		home, err := os.UserHomeDir()
		if err != nil {
			fileHistoryDir = filepath.Join(os.TempDir(), "providence-file-history")
			return
		}
		fileHistoryDir = filepath.Join(home, ".providence", "history")
	})
	fileHistoryDirMu.RLock()
	defer fileHistoryDirMu.RUnlock()
	return fileHistoryDir
}

// Snapshot is the metadata for a single stored snapshot.
type Snapshot struct {
	ID        string    `json:"id"`          // unix-ms timestamp string, also the filename stem
	Path      string    `json:"path"`        // original absolute path
	Bytes     int64     `json:"bytes"`       // uncompressed size
	CreatedAt time.Time `json:"created_at"`  // wall-clock capture time
}

// pathKey hashes the absolute path so snapshots for long or
// filesystem-hostile paths still fit on disk. SHA-256 first 16 hex
// chars is plenty for collision resistance across a single user's
// edits.
func pathKey(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])[:16]
}

// SnapshotFile writes a gzipped copy of the file at path under the
// per-path history directory. Missing source files are treated as a
// no-op (nothing to snapshot for a first-time write). Returns the
// Snapshot metadata on success; empty Snapshot on no-op.
func SnapshotFile(path string) (Snapshot, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return Snapshot{}, nil // nothing to snapshot
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return Snapshot{}, fmt.Errorf("cannot snapshot directory: %s", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read %s: %w", path, err)
	}

	key := pathKey(path)
	dir := filepath.Join(defaultFileHistoryDir(), key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Snapshot{}, fmt.Errorf("mkdir history: %w", err)
	}

	now := time.Now()
	id := fmt.Sprintf("%d", now.UnixMilli())
	gzPath := filepath.Join(dir, id+".gz")

	f, err := os.Create(gzPath)
	if err != nil {
		return Snapshot{}, fmt.Errorf("create snapshot: %w", err)
	}
	zw := gzip.NewWriter(f)
	if _, err := zw.Write(data); err != nil {
		_ = zw.Close()
		_ = f.Close()
		_ = os.Remove(gzPath)
		return Snapshot{}, fmt.Errorf("gzip write: %w", err)
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		_ = os.Remove(gzPath)
		return Snapshot{}, fmt.Errorf("gzip close: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(gzPath)
		return Snapshot{}, fmt.Errorf("close snapshot: %w", err)
	}

	snap := Snapshot{
		ID:        id,
		Path:      path,
		Bytes:     info.Size(),
		CreatedAt: now,
	}

	// Persist metadata alongside the gz blob so List can reconstruct
	// the Snapshot without re-reading the data file.
	metaBytes, _ := json.Marshal(snap)
	_ = os.WriteFile(filepath.Join(dir, id+".json"), metaBytes, 0o644)

	evictOldSnapshots(dir, now)
	return snap, nil
}

// evictOldSnapshots removes snapshots beyond historyMaxPerPath or
// older than historyMaxAge. Called lazily from SnapshotFile so fresh
// writes tidy the directory.
func evictOldSnapshots(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	type pair struct {
		id   string
		when time.Time
	}
	var pairs []pair
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".gz") {
			continue
		}
		id := strings.TrimSuffix(name, ".gz")
		info, err := e.Info()
		if err != nil {
			continue
		}
		pairs = append(pairs, pair{id: id, when: info.ModTime()})
	}

	// Sort descending so index >= historyMaxPerPath gets evicted.
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].when.After(pairs[j].when) })

	for i, p := range pairs {
		tooOld := now.Sub(p.when) > historyMaxAge
		tooMany := i >= historyMaxPerPath
		if tooOld || tooMany {
			_ = os.Remove(filepath.Join(dir, p.id+".gz"))
			_ = os.Remove(filepath.Join(dir, p.id+".json"))
		}
	}
}

// ListSnapshots returns all snapshots for path, newest first.
func ListSnapshots(path string) ([]Snapshot, error) {
	key := pathKey(path)
	dir := filepath.Join(defaultFileHistoryDir(), key)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var snaps []Snapshot
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		metaBytes, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s Snapshot
		if err := json.Unmarshal(metaBytes, &s); err != nil {
			continue
		}
		snaps = append(snaps, s)
	}
	sort.Slice(snaps, func(i, j int) bool {
		return snaps[i].CreatedAt.After(snaps[j].CreatedAt)
	})
	return snaps, nil
}

// RestoreSnapshot replaces the file at path with the contents of
// snapshotID. Returns an error if the snapshot does not exist.
func RestoreSnapshot(path, snapshotID string) error {
	key := pathKey(path)
	gzPath := filepath.Join(defaultFileHistoryDir(), key, snapshotID+".gz")
	f, err := os.Open(gzPath)
	if err != nil {
		return fmt.Errorf("open snapshot %s: %w", snapshotID, err)
	}
	defer f.Close()

	zr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gunzip: %w", err)
	}
	defer zr.Close()

	data, err := io.ReadAll(zr)
	if err != nil {
		return fmt.Errorf("read snapshot: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("restore %s: %w", path, err)
	}
	return nil
}
