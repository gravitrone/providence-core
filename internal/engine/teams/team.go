package teams

import "time"

// Team represents a named group of agents that coordinate via shared task
// lists and file-based mailboxes. CC-compatible data model.
type Team struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	LeadID      string   `json:"lead_id"`
	Members     []Member `json:"members"`
	TaskListDir string   `json:"task_list_dir"`
}

// Member is a single agent participating in a team.
type Member struct {
	AgentID   string    `json:"agent_id"`
	Name      string    `json:"name"`
	AgentType string    `json:"agent_type"`
	Model     string    `json:"model"`
	JoinedAt  time.Time `json:"joined_at"`
	IsActive  bool      `json:"is_active"`
	Color     string    `json:"color"`
}

// HasActiveMember returns true if any member is currently active.
func (t *Team) HasActiveMember() bool {
	for _, m := range t.Members {
		if m.IsActive {
			return true
		}
	}
	return false
}

// FindMember returns the member with the given name, or nil.
func (t *Team) FindMember(name string) *Member {
	for i := range t.Members {
		if t.Members[i].Name == name {
			return &t.Members[i]
		}
	}
	return nil
}

// FindMemberByID returns the member with the given agent ID, or nil.
func (t *Team) FindMemberByID(agentID string) *Member {
	for i := range t.Members {
		if t.Members[i].AgentID == agentID {
			return &t.Members[i]
		}
	}
	return nil
}

// ActiveCount returns the number of active members.
func (t *Team) ActiveCount() int {
	n := 0
	for _, m := range t.Members {
		if m.IsActive {
			n++
		}
	}
	return n
}
