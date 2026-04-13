package teams

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store handles team file persistence at ~/.claude/teams/{team-name}/config.json
// and task lists at ~/.claude/tasks/{team-name}/. CC-compatible paths.
type Store struct {
	baseDir string // ~/.claude
}

// NewStore creates a Store rooted at the given base directory.
// Typically pass the result of os.UserHomeDir() + "/.claude".
func NewStore(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

// DefaultStore creates a Store at ~/.claude.
func DefaultStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home dir: %w", err)
	}
	return NewStore(filepath.Join(home, ".claude")), nil
}

// --- Paths ---

func (s *Store) teamsDir() string {
	return filepath.Join(s.baseDir, "teams")
}

func (s *Store) teamDir(name string) string {
	return filepath.Join(s.teamsDir(), sanitizeName(name))
}

func (s *Store) configPath(name string) string {
	return filepath.Join(s.teamDir(name), "config.json")
}

func (s *Store) tasksDir(name string) string {
	return filepath.Join(s.baseDir, "tasks", sanitizeName(name))
}

func (s *Store) inboxDir(name string) string {
	return filepath.Join(s.teamDir(name), "inboxes")
}

// --- CRUD ---

// Save persists a team to disk.
func (s *Store) Save(team *Team) error {
	dir := s.teamDir(team.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create team dir: %w", err)
	}

	// Ensure task list directory exists.
	taskDir := s.tasksDir(team.Name)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return fmt.Errorf("create tasks dir: %w", err)
	}
	team.TaskListDir = taskDir

	// Ensure inboxes directory exists.
	inboxDir := s.inboxDir(team.Name)
	if err := os.MkdirAll(inboxDir, 0o755); err != nil {
		return fmt.Errorf("create inboxes dir: %w", err)
	}

	data, err := json.MarshalIndent(team, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal team: %w", err)
	}

	if err := os.WriteFile(s.configPath(team.Name), data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

// Load reads a team from disk by name.
func (s *Store) Load(name string) (*Team, error) {
	data, err := os.ReadFile(s.configPath(name))
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var team Team
	if err := json.Unmarshal(data, &team); err != nil {
		return nil, fmt.Errorf("unmarshal team: %w", err)
	}

	return &team, nil
}

// Delete removes a team and all its files (config, inboxes, tasks).
func (s *Store) Delete(name string) error {
	dir := s.teamDir(name)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("team %q not found", name)
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove team dir: %w", err)
	}

	// Clean up tasks directory too.
	taskDir := s.tasksDir(name)
	_ = os.RemoveAll(taskDir) // best-effort

	return nil
}

// List returns all team names found on disk.
func (s *Store) List() ([]string, error) {
	dir := s.teamsDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read teams dir: %w", err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Check that config.json exists.
		cfg := filepath.Join(dir, e.Name(), "config.json")
		if _, err := os.Stat(cfg); err == nil {
			names = append(names, e.Name())
		}
	}

	return names, nil
}

// Exists returns true if a team with the given name exists on disk.
func (s *Store) Exists(name string) bool {
	_, err := os.Stat(s.configPath(name))
	return err == nil
}

// CreateTeam creates a new team with sensible defaults and saves it.
func (s *Store) CreateTeam(name, description string) (*Team, error) {
	if s.Exists(name) {
		return nil, fmt.Errorf("team %q already exists", name)
	}

	team := &Team{
		Name:        name,
		Description: description,
		CreatedAt:   time.Now(),
		Members:     []Member{},
	}

	if err := s.Save(team); err != nil {
		return nil, err
	}

	return team, nil
}

// --- Helpers ---

// sanitizeName converts a team name into a filesystem-safe slug.
func sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")

	var clean []byte
	for _, b := range []byte(name) {
		if (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '-' || b == '_' {
			clean = append(clean, b)
		}
	}

	result := string(clean)
	if result == "" {
		result = "unnamed"
	}
	return result
}
