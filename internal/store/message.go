package store

import "time"

// MessageRow represents a row from the messages table.
type MessageRow struct {
	ID         int64
	SessionID  string
	Role       string
	Content    string
	ToolName   string
	ToolArgs   string
	ToolStatus string
	ToolBody   string
	ToolOutput string
	ImageCount int
	Done       bool
	CreatedAt  time.Time
}

// AddMessage inserts a message and returns its ID.
func (s *Store) AddMessage(sessionID, role, content, toolName, toolArgs, toolStatus, toolBody, toolOutput string, imageCount int, done bool) (int64, error) {
	if s == nil {
		return 0, nil
	}
	res, err := s.db.Exec(
		`INSERT INTO messages (session_id, role, content, tool_name, tool_args, tool_status, tool_body, tool_output, image_count, done)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID, role, content, toolName, toolArgs, toolStatus, toolBody, toolOutput, imageCount, done,
	)
	if err != nil {
		return 0, err
	}
	// Bump session timestamp
	s.UpdateSessionTimestamp(sessionID)
	return res.LastInsertId()
}

// UpdateMessageContent updates content and done flag for a streaming message.
func (s *Store) UpdateMessageContent(id int64, content string, done bool) error {
	if s == nil {
		return nil
	}
	_, err := s.db.Exec(`UPDATE messages SET content = ?, done = ? WHERE id = ?`, content, done, id)
	return err
}

// UpdateToolOutput sets tool output and status on an existing message.
func (s *Store) UpdateToolOutput(id int64, output, status string) error {
	if s == nil {
		return nil
	}
	_, err := s.db.Exec(`UPDATE messages SET tool_output = ?, tool_status = ? WHERE id = ?`, output, status, id)
	return err
}

// GetMessages returns all messages for a session, ordered by creation time.
func (s *Store) GetMessages(sessionID string) ([]MessageRow, error) {
	if s == nil {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT id, session_id, role, COALESCE(content,''), COALESCE(tool_name,''), COALESCE(tool_args,''),
		 COALESCE(tool_status,''), COALESCE(tool_body,''), COALESCE(tool_output,''), image_count, done, created_at
		 FROM messages WHERE session_id = ? ORDER BY created_at ASC, id ASC`, sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []MessageRow
	for rows.Next() {
		var m MessageRow
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.ToolName, &m.ToolArgs,
			&m.ToolStatus, &m.ToolBody, &m.ToolOutput, &m.ImageCount, &m.Done, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// DeleteMessages removes all messages for a session.
func (s *Store) DeleteMessages(sessionID string) error {
	if s == nil {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM messages WHERE session_id = ?`, sessionID)
	return err
}
