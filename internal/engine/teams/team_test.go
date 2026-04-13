package teams

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTeamHasActiveMember(t *testing.T) {
	team := &Team{
		Members: []Member{
			{Name: "alice", IsActive: false},
			{Name: "bob", IsActive: true},
		},
	}
	assert.True(t, team.HasActiveMember())

	team.Members[1].IsActive = false
	assert.False(t, team.HasActiveMember())
}

func TestTeamFindMember(t *testing.T) {
	team := &Team{
		Members: []Member{
			{Name: "alice", AgentID: "agent-1"},
			{Name: "bob", AgentID: "agent-2"},
		},
	}

	m := team.FindMember("alice")
	assert.NotNil(t, m)
	assert.Equal(t, "agent-1", m.AgentID)

	m = team.FindMember("charlie")
	assert.Nil(t, m)
}

func TestTeamFindMemberByID(t *testing.T) {
	team := &Team{
		Members: []Member{
			{Name: "alice", AgentID: "agent-1"},
		},
	}

	m := team.FindMemberByID("agent-1")
	assert.NotNil(t, m)
	assert.Equal(t, "alice", m.Name)

	m = team.FindMemberByID("agent-999")
	assert.Nil(t, m)
}

func TestTeamActiveCount(t *testing.T) {
	team := &Team{
		Members: []Member{
			{Name: "alice", IsActive: true},
			{Name: "bob", IsActive: false},
			{Name: "charlie", IsActive: true},
		},
	}
	assert.Equal(t, 2, team.ActiveCount())
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my-team", "my-team"},
		{"My Team", "my-team"},
		{"foo bar!@#baz", "foo-barbaz"},
		{"", "unnamed"},
		{"123", "123"},
		{"a_b-c", "a_b-c"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, sanitizeName(tt.input), "input: %q", tt.input)
	}
}

func TestTeamMemberFields(t *testing.T) {
	m := Member{
		AgentID:   "agent-abc",
		Name:      "researcher",
		AgentType: "research",
		Model:     "claude-sonnet-4",
		JoinedAt:  time.Now(),
		IsActive:  true,
		Color:     "#ff5733",
	}
	assert.Equal(t, "researcher", m.Name)
	assert.True(t, m.IsActive)
}
