package ember

import (
	"os"

	"gopkg.in/yaml.v3"
)

// --- Task Queue ---

// TaskQueueItem is a task from .providence/tasks.yaml.
type TaskQueueItem struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
	Priority    int    `yaml:"priority"`
	Status      string `yaml:"status"` // pending, running, completed, failed
	Prompt      string `yaml:"prompt"` // what to tell the model
}

// TaskQueue manages tasks from a YAML file.
type TaskQueue struct {
	FilePath string
	Tasks    []TaskQueueItem
}

// Load reads tasks from the YAML file.
func (q *TaskQueue) Load() error {
	data, err := os.ReadFile(q.FilePath)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, &q.Tasks)
}

// Save writes current state back to YAML.
func (q *TaskQueue) Save() error {
	data, err := yaml.Marshal(q.Tasks)
	if err != nil {
		return err
	}
	return os.WriteFile(q.FilePath, data, 0644)
}

// NextPending returns the next pending task (first by slice order).
func (q *TaskQueue) NextPending() *TaskQueueItem {
	for i := range q.Tasks {
		if q.Tasks[i].Status == "pending" {
			return &q.Tasks[i]
		}
	}
	return nil
}

// MarkRunning sets a task's status to running.
func (q *TaskQueue) MarkRunning(id string) {
	for i := range q.Tasks {
		if q.Tasks[i].ID == id {
			q.Tasks[i].Status = "running"
			return
		}
	}
}

// MarkCompleted sets a task's status to completed.
func (q *TaskQueue) MarkCompleted(id string) {
	for i := range q.Tasks {
		if q.Tasks[i].ID == id {
			q.Tasks[i].Status = "completed"
			return
		}
	}
}

// MarkFailed sets a task's status to failed.
func (q *TaskQueue) MarkFailed(id string) {
	for i := range q.Tasks {
		if q.Tasks[i].ID == id {
			q.Tasks[i].Status = "failed"
			return
		}
	}
}
