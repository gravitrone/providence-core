package store

import (
	"fmt"
	"time"
)

// Event kind constants for the message_events side table. Persisted state is
// reconstructed on resume so the UI does not have to re-run the code paths
// that originally produced these records.
const (
	EventKindFileSnapshot       = "file_snapshot"
	EventKindContentReplacement = "content_replacement"
	EventKindWorktree           = "worktree"
	EventKindToolCallID         = "tool_call_id"
)

// MessageEvent is one row from the message_events side table. Payload holds
// a JSON-encoded blob whose shape depends on Kind.
type MessageEvent struct {
	ID        int64
	SessionID string
	Seq       int64
	Kind      string
	Payload   string
	CreatedAt time.Time
}

// AddMessageEvent appends a typed side-channel event for a session. Seq is
// provided by the caller so ordering matches the logical write order.
func (s *Store) AddMessageEvent(sessionID string, seq int64, kind, payloadJSON string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	res, err := s.db.Exec(
		`INSERT INTO message_events (session_id, seq, kind, payload) VALUES (?, ?, ?, ?)`,
		sessionID, seq, kind, payloadJSON,
	)
	if err != nil {
		return 0, fmt.Errorf("insert message_event: %w", err)
	}
	return res.LastInsertId()
}

// GetSessionEvents returns all side-channel events for the given session
// ordered by (seq, id). Returns an empty slice when the table is empty or
// when no events were recorded for the session; callers treat that as a
// back-compat signal and skip hydration.
func (s *Store) GetSessionEvents(sessionID string) ([]MessageEvent, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT id, session_id, seq, kind, COALESCE(payload,''), created_at
		 FROM message_events WHERE session_id = ? ORDER BY seq ASC, id ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query message_events: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var events []MessageEvent
	for rows.Next() {
		var ev MessageEvent
		if err := rows.Scan(&ev.ID, &ev.SessionID, &ev.Seq, &ev.Kind, &ev.Payload, &ev.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message_event: %w", err)
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}

// DeleteSessionEvents removes every event for a session. Used by tests and
// forthcoming retention paths. Safe to call on sessions with no events.
func (s *Store) DeleteSessionEvents(sessionID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM message_events WHERE session_id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("delete message_events: %w", err)
	}
	return nil
}
