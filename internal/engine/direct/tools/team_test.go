package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gravitrone/providence-core/internal/engine/teams"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tempTeamStore(t *testing.T) *teams.Store {
	t.Helper()
	return teams.NewStore(t.TempDir())
}

func TestTeamCreateToolMetadata(t *testing.T) {
	s := tempTeamStore(t)
	tool := NewTeamCreateTool(s)

	assert.Equal(t, "TeamCreate", tool.Name())
	assert.False(t, tool.ReadOnly())
	assert.NotEmpty(t, tool.Description())

	schema := tool.InputSchema()
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	_, hasName := props["team_name"]
	assert.True(t, hasName)
}

func TestTeamCreateToolExecute(t *testing.T) {
	s := tempTeamStore(t)
	tool := NewTeamCreateTool(s)

	result := tool.Execute(context.Background(), map[string]any{
		"team_name":   "builders",
		"description": "build squad",
	})
	assert.False(t, result.IsError)

	var resp map[string]any
	err := json.Unmarshal([]byte(result.Content), &resp)
	require.NoError(t, err)
	assert.Equal(t, "created", resp["status"])
	assert.Equal(t, "builders", resp["team_name"])

	// Verify it was persisted.
	assert.True(t, s.Exists("builders"))
}

func TestTeamCreateToolMissingName(t *testing.T) {
	s := tempTeamStore(t)
	tool := NewTeamCreateTool(s)

	result := tool.Execute(context.Background(), map[string]any{})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "required")
}

func TestTeamCreateToolDuplicate(t *testing.T) {
	s := tempTeamStore(t)
	tool := NewTeamCreateTool(s)

	result := tool.Execute(context.Background(), map[string]any{
		"team_name": "alpha",
	})
	assert.False(t, result.IsError)

	result = tool.Execute(context.Background(), map[string]any{
		"team_name": "alpha",
	})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "already exists")
}

func TestTeamCreateToolPrompt(t *testing.T) {
	s := tempTeamStore(t)
	tool := NewTeamCreateTool(s)
	assert.NotEmpty(t, tool.Prompt())
}

func TestTeamDeleteToolMetadata(t *testing.T) {
	s := tempTeamStore(t)
	tool := NewTeamDeleteTool(s)

	assert.Equal(t, "TeamDelete", tool.Name())
	assert.False(t, tool.ReadOnly())
	assert.NotEmpty(t, tool.Description())
}

func TestTeamDeleteToolExecute(t *testing.T) {
	s := tempTeamStore(t)

	// Create a team first.
	_, err := s.CreateTeam("doomed", "to be deleted")
	require.NoError(t, err)

	tool := NewTeamDeleteTool(s)
	result := tool.Execute(context.Background(), map[string]any{
		"team_name": "doomed",
	})
	assert.False(t, result.IsError)

	var resp map[string]any
	err = json.Unmarshal([]byte(result.Content), &resp)
	require.NoError(t, err)
	assert.Equal(t, "deleted", resp["status"])
	assert.False(t, s.Exists("doomed"))
}

func TestTeamDeleteToolNotFound(t *testing.T) {
	s := tempTeamStore(t)
	tool := NewTeamDeleteTool(s)

	result := tool.Execute(context.Background(), map[string]any{
		"team_name": "ghost",
	})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "not found")
}

func TestTeamDeleteToolActiveMembers(t *testing.T) {
	s := tempTeamStore(t)

	// Create a team with an active member.
	team, err := s.CreateTeam("busy", "active team")
	require.NoError(t, err)
	team.Members = append(team.Members, teams.Member{
		AgentID:  "agent-1",
		Name:     "worker",
		IsActive: true,
	})
	err = s.Save(team)
	require.NoError(t, err)

	tool := NewTeamDeleteTool(s)
	result := tool.Execute(context.Background(), map[string]any{
		"team_name": "busy",
	})
	assert.True(t, result.IsError)

	var resp map[string]any
	err = json.Unmarshal([]byte(result.Content), &resp)
	require.NoError(t, err)
	assert.Equal(t, "rejected", resp["status"])
	assert.Equal(t, float64(1), resp["active_members"])

	// Team should still exist.
	assert.True(t, s.Exists("busy"))
}

func TestTeamDeleteToolMissingName(t *testing.T) {
	s := tempTeamStore(t)
	tool := NewTeamDeleteTool(s)

	result := tool.Execute(context.Background(), map[string]any{})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "required")
}
