package teams

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Message is a single inter-agent message stored in a file-based mailbox.
type Message struct {
	From      string    `json:"from"`
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
	Read      bool      `json:"read"`
	Summary   string    `json:"summary"`
}

// Mailbox provides file-based message passing between team members.
// Each agent gets an inbox at ~/.claude/teams/{team}/inboxes/{name}.json.
// Uses atomic write (write-to-temp + rename) for concurrent safety.
type Mailbox struct {
	store *Store
}

// NewMailbox creates a Mailbox backed by the given Store.
func NewMailbox(store *Store) *Mailbox {
	return &Mailbox{store: store}
}

// WriteToMailbox appends a message to the recipient's inbox file.
func (m *Mailbox) WriteToMailbox(teamName, recipientName string, msg Message) error {
	inboxPath := m.inboxPath(teamName, recipientName)

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(inboxPath), 0o755); err != nil {
		return fmt.Errorf("create inbox dir: %w", err)
	}

	// Load existing messages.
	messages, err := m.loadInbox(inboxPath)
	if err != nil {
		return fmt.Errorf("load inbox: %w", err)
	}

	messages = append(messages, msg)

	// Atomic write: temp file + rename.
	if err := m.atomicWrite(inboxPath, messages); err != nil {
		return fmt.Errorf("write inbox: %w", err)
	}

	return nil
}

// ReadUnread returns all unread messages from the agent's inbox.
func (m *Mailbox) ReadUnread(teamName, agentName string) ([]Message, error) {
	inboxPath := m.inboxPath(teamName, agentName)

	messages, err := m.loadInbox(inboxPath)
	if err != nil {
		return nil, fmt.Errorf("load inbox: %w", err)
	}

	var unread []Message
	for _, msg := range messages {
		if !msg.Read {
			unread = append(unread, msg)
		}
	}

	return unread, nil
}

// MarkRead marks all messages in the agent's inbox as read.
func (m *Mailbox) MarkRead(teamName, agentName string) error {
	inboxPath := m.inboxPath(teamName, agentName)

	messages, err := m.loadInbox(inboxPath)
	if err != nil {
		return fmt.Errorf("load inbox: %w", err)
	}

	changed := false
	for i := range messages {
		if !messages[i].Read {
			messages[i].Read = true
			changed = true
		}
	}

	if !changed {
		return nil
	}

	return m.atomicWrite(inboxPath, messages)
}

// ClearInbox removes all messages from the agent's inbox.
func (m *Mailbox) ClearInbox(teamName, agentName string) error {
	inboxPath := m.inboxPath(teamName, agentName)
	if err := os.Remove(inboxPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear inbox: %w", err)
	}
	return nil
}

// --- Internal ---

func (m *Mailbox) inboxPath(teamName, agentName string) string {
	return filepath.Join(
		m.store.inboxDir(teamName),
		sanitizeName(agentName)+".json",
	)
}

func (m *Mailbox) loadInbox(path string) ([]Message, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var messages []Message
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, fmt.Errorf("unmarshal inbox: %w", err)
	}

	return messages, nil
}

func (m *Mailbox) atomicWrite(path string, messages []Message) error {
	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal messages: %w", err)
	}

	// Write to temp file in same directory, then rename for atomicity.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename temp: %w", err)
	}

	return nil
}
