package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Store wraps a SQLite database for session persistence.
type Store struct {
	db *sql.DB
}

// DefaultDBPath returns ~/.providence/sessions.db.
func DefaultDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".providence", "sessions.db")
}

// Open creates or opens the SQLite database at dbPath.
func Open(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")
	db.Exec("PRAGMA busy_timeout=5000")
	db.Exec("PRAGMA synchronous=NORMAL")
	db.Exec("PRAGMA cache_size=-8000")

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func migrate(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		cwd TEXT NOT NULL,
		engine_type TEXT,
		model TEXT,
		title TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		token_count INTEGER DEFAULT 0,
		cost_usd REAL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
		role TEXT NOT NULL,
		content TEXT,
		tool_name TEXT,
		tool_args TEXT,
		tool_status TEXT,
		tool_body TEXT,
		tool_output TEXT,
		image_count INTEGER DEFAULT 0,
		done BOOLEAN DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_cwd ON sessions(cwd);
	CREATE INDEX IF NOT EXISTS idx_sessions_updated ON sessions(updated_at DESC);
	`
	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	// FTS5 virtual table for full-text search across messages.
	// Separate exec because some SQLite builds don't support mixing
	// DDL types in one Exec call.
	fts := `
	CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
		content,
		content='messages',
		content_rowid='id'
	);

	CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
		INSERT INTO messages_fts(rowid, content) VALUES (new.id, new.content);
	END;
	CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
		INSERT INTO messages_fts(messages_fts, rowid, content) VALUES('delete', old.id, old.content);
	END;
	CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE ON messages BEGIN
		INSERT INTO messages_fts(messages_fts, rowid, content) VALUES('delete', old.id, old.content);
		INSERT INTO messages_fts(rowid, content) VALUES (new.id, new.content);
	END;
	`
	_, err = db.Exec(fts)
	if err != nil {
		return err
	}

	// Add learnings column if not present (idempotent migration).
	_, err = db.Exec(`ALTER TABLE sessions ADD COLUMN learnings TEXT`)
	if err != nil {
		if !isDuplicateColumnErr(err) {
			return err
		}
	}

	// Add tags column if not present (idempotent migration).
	_, err = db.Exec(`ALTER TABLE sessions ADD COLUMN tags TEXT`)
	if err != nil {
		if !isDuplicateColumnErr(err) {
			return err
		}
	}

	return nil
}

// isDuplicateColumnErr returns true when SQLite complains that a column already exists.
func isDuplicateColumnErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate column name") || strings.Contains(msg, "column already exists")
}
