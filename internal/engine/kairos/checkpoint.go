package kairos

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Checkpoint captures session state for crash recovery.
type Checkpoint struct {
	SessionID   string    `json:"session_id"`
	EngineType  string    `json:"engine_type"`
	Model       string    `json:"model"`
	TurnCount   int       `json:"turn_count"`
	TokenCount  int       `json:"token_count"`
	LastTaskID  string    `json:"last_task_id"`
	KairosState string    `json:"kairos_state"` // active, paused
	CreatedAt   time.Time `json:"created_at"`
}

// SaveCheckpoint writes state to .providence/checkpoint.json.
func SaveCheckpoint(dir string, cp Checkpoint) error {
	provDir := filepath.Join(dir, ".providence")
	if err := os.MkdirAll(provDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(provDir, "checkpoint.json")
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadCheckpoint reads state from .providence/checkpoint.json.
func LoadCheckpoint(dir string) (*Checkpoint, error) {
	path := filepath.Join(dir, ".providence", "checkpoint.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

// ClearCheckpoint removes the checkpoint file.
func ClearCheckpoint(dir string) error {
	path := filepath.Join(dir, ".providence", "checkpoint.json")
	return os.Remove(path)
}
