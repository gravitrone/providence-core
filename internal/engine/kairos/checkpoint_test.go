package kairos

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveLoadCheckpoint(t *testing.T) {
	dir := t.TempDir()

	cp := Checkpoint{
		SessionID:   "sess-123",
		EngineType:  "direct",
		Model:       "claude-sonnet-4-20250514",
		TurnCount:   42,
		TokenCount:  8192,
		LastTaskID:  "task-7",
		KairosState: "active",
		CreatedAt:   time.Now().Truncate(time.Second),
	}

	require.NoError(t, SaveCheckpoint(dir, cp))

	// Verify the file was created.
	path := filepath.Join(dir, ".providence", "checkpoint.json")
	_, err := os.Stat(path)
	require.NoError(t, err)

	// Load it back.
	loaded, err := LoadCheckpoint(dir)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, cp.SessionID, loaded.SessionID)
	assert.Equal(t, cp.EngineType, loaded.EngineType)
	assert.Equal(t, cp.Model, loaded.Model)
	assert.Equal(t, cp.TurnCount, loaded.TurnCount)
	assert.Equal(t, cp.TokenCount, loaded.TokenCount)
	assert.Equal(t, cp.LastTaskID, loaded.LastTaskID)
	assert.Equal(t, cp.KairosState, loaded.KairosState)
	// Time comparison with truncation to avoid nanosecond mismatch.
	assert.Equal(t, cp.CreatedAt.Unix(), loaded.CreatedAt.Unix())
}

func TestLoadCheckpointMissing(t *testing.T) {
	dir := t.TempDir()
	loaded, err := LoadCheckpoint(dir)
	assert.Error(t, err)
	assert.Nil(t, loaded)
}

func TestClearCheckpoint(t *testing.T) {
	dir := t.TempDir()

	cp := Checkpoint{
		SessionID:   "sess-456",
		KairosState: "paused",
		CreatedAt:   time.Now(),
	}
	require.NoError(t, SaveCheckpoint(dir, cp))

	// Clear it.
	require.NoError(t, ClearCheckpoint(dir))

	// Should no longer exist.
	path := filepath.Join(dir, ".providence", "checkpoint.json")
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}
