package store

import "time"

// SessionRow represents a row from the sessions table.
type SessionRow struct {
	ID           string
	CWD          string
	EngineType   string
	Model        string
	Title        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	TokenCount   int
	CostUSD      float64
	MessageCount int
}

// CreateSession inserts a new session.
func (s *Store) CreateSession(id, cwd, engineType, model string) error {
	if s == nil {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, cwd, engine_type, model) VALUES (?, ?, ?, ?)`,
		id, cwd, engineType, model,
	)
	return err
}

// UpdateSessionTitle sets the session title.
func (s *Store) UpdateSessionTitle(id, title string) error {
	if s == nil {
		return nil
	}
	_, err := s.db.Exec(`UPDATE sessions SET title = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, title, id)
	return err
}

// UpdateSessionTimestamp bumps updated_at to now.
func (s *Store) UpdateSessionTimestamp(id string) error {
	if s == nil {
		return nil
	}
	_, err := s.db.Exec(`UPDATE sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	return err
}

// DeleteSession removes a session and its messages (cascade).
func (s *Store) DeleteSession(id string) error {
	if s == nil {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// GetSession returns a single session by ID.
func (s *Store) GetSession(id string) (*SessionRow, error) {
	if s == nil {
		return nil, nil
	}
	row := s.db.QueryRow(
		`SELECT id, cwd, engine_type, model, COALESCE(title,''), created_at, updated_at, token_count, cost_usd FROM sessions WHERE id = ?`, id,
	)
	var sr SessionRow
	err := row.Scan(&sr.ID, &sr.CWD, &sr.EngineType, &sr.Model, &sr.Title, &sr.CreatedAt, &sr.UpdatedAt, &sr.TokenCount, &sr.CostUSD)
	if err != nil {
		return nil, err
	}
	return &sr, nil
}

// SaveSessionLearnings stores a JSON learnings blob for a session.
func (s *Store) SaveSessionLearnings(id, learningsJSON string) error {
	if s == nil {
		return nil
	}
	_, err := s.db.Exec(`UPDATE sessions SET learnings = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, learningsJSON, id)
	return err
}

// ListSessions returns recent sessions, optionally filtered by CWD.
// If cwd is empty, returns all sessions.
func (s *Store) ListSessions(cwd string, limit int) ([]SessionRow, error) {
	if s == nil {
		return nil, nil
	}
	var rows []SessionRow
	var query string
	var args []any

	if cwd != "" {
		query = `SELECT s.id, s.cwd, s.engine_type, s.model, COALESCE(s.title,''), s.created_at, s.updated_at, s.token_count, s.cost_usd,
			(SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id) as msg_count
			FROM sessions s WHERE s.cwd = ? ORDER BY s.updated_at DESC LIMIT ?`
		args = []any{cwd, limit}
	} else {
		query = `SELECT s.id, s.cwd, s.engine_type, s.model, COALESCE(s.title,''), s.created_at, s.updated_at, s.token_count, s.cost_usd,
			(SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id) as msg_count
			FROM sessions s ORDER BY s.updated_at DESC LIMIT ?`
		args = []any{limit}
	}

	dbRows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer dbRows.Close()

	for dbRows.Next() {
		var sr SessionRow
		if err := dbRows.Scan(&sr.ID, &sr.CWD, &sr.EngineType, &sr.Model, &sr.Title, &sr.CreatedAt, &sr.UpdatedAt, &sr.TokenCount, &sr.CostUSD, &sr.MessageCount); err != nil {
			return nil, err
		}
		rows = append(rows, sr)
	}
	return rows, dbRows.Err()
}
