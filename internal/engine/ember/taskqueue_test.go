package ember

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testTasksYAML = `- id: task-1
  description: "Build the thing"
  priority: 1
  status: completed
  prompt: "Build feature X"
- id: task-2
  description: "Test the thing"
  priority: 2
  status: pending
  prompt: "Run tests for feature X"
- id: task-3
  description: "Deploy the thing"
  priority: 3
  status: pending
  prompt: "Deploy feature X"
`

func TestLoadTaskQueue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yaml")
	require.NoError(t, os.WriteFile(path, []byte(testTasksYAML), 0644))

	q := &TaskQueue{FilePath: path}
	require.NoError(t, q.Load())

	assert.Len(t, q.Tasks, 3)
	assert.Equal(t, "task-1", q.Tasks[0].ID)
	assert.Equal(t, "completed", q.Tasks[0].Status)
	assert.Equal(t, "Build the thing", q.Tasks[0].Description)
}

func TestNextPending(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yaml")
	require.NoError(t, os.WriteFile(path, []byte(testTasksYAML), 0644))

	q := &TaskQueue{FilePath: path}
	require.NoError(t, q.Load())

	next := q.NextPending()
	require.NotNil(t, next)
	assert.Equal(t, "task-2", next.ID)
}

func TestNextPendingNoneLeft(t *testing.T) {
	q := &TaskQueue{
		Tasks: []TaskQueueItem{
			{ID: "done", Status: "completed"},
		},
	}
	assert.Nil(t, q.NextPending())
}

func TestMarkRunning(t *testing.T) {
	q := &TaskQueue{
		Tasks: []TaskQueueItem{
			{ID: "t1", Status: "pending"},
		},
	}
	q.MarkRunning("t1")
	assert.Equal(t, "running", q.Tasks[0].Status)
}

func TestMarkCompleted(t *testing.T) {
	q := &TaskQueue{
		Tasks: []TaskQueueItem{
			{ID: "t1", Status: "running"},
		},
	}
	q.MarkCompleted("t1")
	assert.Equal(t, "completed", q.Tasks[0].Status)
}

func TestMarkFailed(t *testing.T) {
	q := &TaskQueue{
		Tasks: []TaskQueueItem{
			{ID: "t1", Status: "running"},
		},
	}
	q.MarkFailed("t1")
	assert.Equal(t, "failed", q.Tasks[0].Status)
}

func TestSaveTaskQueue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yaml")

	q := &TaskQueue{
		FilePath: path,
		Tasks: []TaskQueueItem{
			{ID: "save-1", Description: "Saved task", Priority: 1, Status: "pending", Prompt: "do it"},
		},
	}
	require.NoError(t, q.Save())

	// Reload and verify roundtrip.
	q2 := &TaskQueue{FilePath: path}
	require.NoError(t, q2.Load())
	assert.Len(t, q2.Tasks, 1)
	assert.Equal(t, "save-1", q2.Tasks[0].ID)
	assert.Equal(t, "pending", q2.Tasks[0].Status)
}
