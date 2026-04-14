package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gravitrone/providence-core/internal/engine/teams"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTeamDeleteTool_HappyPath(t *testing.T) {
	s := tempTeamStore(t)
	_, err := s.CreateTeam("alpha-squad", "test team")
	require.NoError(t, err)

	tool := NewTeamDeleteTool(s)
	result := tool.Execute(context.Background(), map[string]any{
		"team_name": "alpha-squad",
	})

	require.False(t, result.IsError)

	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &resp))
	assert.Equal(t, "deleted", resp["status"])
	assert.Equal(t, "alpha-squad", resp["team_name"])

	// Verify actually removed from disk.
	assert.False(t, s.Exists("alpha-squad"))
}

func TestTeamDeleteTool_MissingTeamReturnsError(t *testing.T) {
	s := tempTeamStore(t)
	tool := NewTeamDeleteTool(s)

	result := tool.Execute(context.Background(), map[string]any{
		"team_name": "nonexistent-team",
	})

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "not found")
}

func TestTeamDeleteTool_InvalidArgsRejected(t *testing.T) {
	s := tempTeamStore(t)
	tool := NewTeamDeleteTool(s)

	// Missing team_name entirely.
	result := tool.Execute(context.Background(), map[string]any{})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "required")

	// Empty string is also invalid.
	result = tool.Execute(context.Background(), map[string]any{
		"team_name": "",
	})
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "required")
}

func TestTeamDeleteTool_ActiveMembersBlocksDeletion(t *testing.T) {
	s := tempTeamStore(t)

	team, err := s.CreateTeam("live-team", "has active agents")
	require.NoError(t, err)
	team.Members = []teams.Member{
		{AgentID: "a1", Name: "worker-1", IsActive: true},
		{AgentID: "a2", Name: "worker-2", IsActive: false},
	}
	require.NoError(t, s.Save(team))

	tool := NewTeamDeleteTool(s)
	result := tool.Execute(context.Background(), map[string]any{
		"team_name": "live-team",
	})

	assert.True(t, result.IsError)

	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &resp))
	assert.Equal(t, "rejected", resp["status"])
	assert.Equal(t, "team has active members", resp["reason"])
	assert.Equal(t, float64(1), resp["active_members"])
	assert.Equal(t, "live-team", resp["team_name"])

	// Team must still exist after rejected delete.
	assert.True(t, s.Exists("live-team"))
}

func TestTeamDeleteTool_InputSchema(t *testing.T) {
	s := tempTeamStore(t)
	tool := NewTeamDeleteTool(s)

	schema := tool.InputSchema()
	require.NotNil(t, schema)

	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	_, hasTeamName := props["team_name"]
	assert.True(t, hasTeamName)

	required, ok := schema["required"].([]string)
	require.True(t, ok)
	assert.Contains(t, required, "team_name")
}

func TestTeamDeleteTool_DeletesAllMembersInactive(t *testing.T) {
	// A team with only inactive members should be deletable.
	s := tempTeamStore(t)

	team, err := s.CreateTeam("retired-team", "all inactive")
	require.NoError(t, err)
	team.Members = []teams.Member{
		{AgentID: "x1", Name: "ex-worker", IsActive: false},
	}
	require.NoError(t, s.Save(team))

	tool := NewTeamDeleteTool(s)
	result := tool.Execute(context.Background(), map[string]any{
		"team_name": "retired-team",
	})

	assert.False(t, result.IsError)
	assert.False(t, s.Exists("retired-team"))
}
