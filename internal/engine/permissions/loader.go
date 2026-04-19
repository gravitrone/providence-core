package permissions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// --- Durable Loader ---

// storeEnvVar lets tests redirect the persistence root.
const storeEnvVar = "PROVIDENCE_PERMISSIONS_DIR"

// ruleFile is the on-disk serialization format for persisted rules.
type ruleFile struct {
	Version int    `json:"version"`
	Project string `json:"project"`
	Rules   []Rule `json:"rules"`
}

// SaveRules atomically writes the given rules to the per-project store.
// The filename is derived from a sha256 hash of projectPath so different
// projects never collide. Writes happen via tempfile + rename in the same
// directory so a crash mid-write leaves the previous version intact.
func SaveRules(projectPath string, rules []Rule) error {
	dir, err := storeDir()
	if err != nil {
		return fmt.Errorf("resolve permissions store: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create permissions store: %w", err)
	}
	payload := ruleFile{Version: 1, Project: projectPath, Rules: rules}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal rules: %w", err)
	}
	target := ruleFilePath(dir, projectPath)
	tmp, err := os.CreateTemp(dir, "rules-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp rules file: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if the rename below fails.
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp rules: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp rules: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp rules: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("rename rules file: %w", err)
	}
	return nil
}

// LoadRules reads the persisted rules for the given project path. A missing
// file is not an error: callers receive a nil slice and nil error so first
// runs start with an empty rule set.
func LoadRules(projectPath string) ([]Rule, error) {
	dir, err := storeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve permissions store: %w", err)
	}
	target := ruleFilePath(dir, projectPath)
	data, err := os.ReadFile(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read rules file: %w", err)
	}
	var payload ruleFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode rules file: %w", err)
	}
	return payload.Rules, nil
}

// storeDir resolves the directory used for persisted rule files. Tests may
// override via the PROVIDENCE_PERMISSIONS_DIR env var. In production the
// directory is ~/.providence/permissions.
func storeDir() (string, error) {
	if override := os.Getenv(storeEnvVar); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}
	return filepath.Join(home, ".providence", "permissions"), nil
}

// ruleFilePath derives the stable filename for a project's rule set. The
// filename is a sha256 hex digest of the absolute project path so two
// projects with similar names never collide.
func ruleFilePath(dir, projectPath string) string {
	sum := sha256.Sum256([]byte(projectPath))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".json")
}
