package teams

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return NewStore(dir)
}

func TestStoreCreateAndLoad(t *testing.T) {
	s := tempStore(t)

	team, err := s.CreateTeam("researchers", "research team")
	require.NoError(t, err)
	assert.Equal(t, "researchers", team.Name)
	assert.Equal(t, "research team", team.Description)
	assert.NotZero(t, team.CreatedAt)

	// Load it back.
	loaded, err := s.Load("researchers")
	require.NoError(t, err)
	assert.Equal(t, team.Name, loaded.Name)
	assert.Equal(t, team.Description, loaded.Description)
}

func TestStoreCreateDuplicate(t *testing.T) {
	s := tempStore(t)

	_, err := s.CreateTeam("alpha", "first")
	require.NoError(t, err)

	_, err = s.CreateTeam("alpha", "second")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestStoreList(t *testing.T) {
	s := tempStore(t)

	_, err := s.CreateTeam("alpha", "a")
	require.NoError(t, err)
	_, err = s.CreateTeam("beta", "b")
	require.NoError(t, err)

	names, err := s.List()
	require.NoError(t, err)
	assert.Len(t, names, 2)
	assert.Contains(t, names, "alpha")
	assert.Contains(t, names, "beta")
}

func TestStoreListEmpty(t *testing.T) {
	s := tempStore(t)

	names, err := s.List()
	require.NoError(t, err)
	assert.Empty(t, names)
}

func TestStoreDelete(t *testing.T) {
	s := tempStore(t)

	_, err := s.CreateTeam("doomed", "to be deleted")
	require.NoError(t, err)
	assert.True(t, s.Exists("doomed"))

	err = s.Delete("doomed")
	require.NoError(t, err)
	assert.False(t, s.Exists("doomed"))
}

func TestStoreDeleteNotFound(t *testing.T) {
	s := tempStore(t)

	err := s.Delete("ghost")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestStoreCreatesDirectories(t *testing.T) {
	s := tempStore(t)

	team, err := s.CreateTeam("builders", "build stuff")
	require.NoError(t, err)

	// Check team dir exists.
	teamDir := s.teamDir("builders")
	_, err = os.Stat(teamDir)
	assert.NoError(t, err)

	// Check tasks dir exists.
	_, err = os.Stat(team.TaskListDir)
	assert.NoError(t, err)

	// Check inboxes dir exists.
	inboxDir := s.inboxDir("builders")
	_, err = os.Stat(inboxDir)
	assert.NoError(t, err)
}

func TestStoreSaveWithMembers(t *testing.T) {
	s := tempStore(t)

	team, err := s.CreateTeam("squad", "test squad")
	require.NoError(t, err)

	team.Members = append(team.Members, Member{
		AgentID:  "agent-1",
		Name:     "alice",
		IsActive: true,
		Color:    "#ff0000",
	})
	err = s.Save(team)
	require.NoError(t, err)

	loaded, err := s.Load("squad")
	require.NoError(t, err)
	require.Len(t, loaded.Members, 1)
	assert.Equal(t, "alice", loaded.Members[0].Name)
	assert.True(t, loaded.Members[0].IsActive)
}

func TestStoreExists(t *testing.T) {
	s := tempStore(t)
	assert.False(t, s.Exists("nope"))

	_, err := s.CreateTeam("yep", "exists")
	require.NoError(t, err)
	assert.True(t, s.Exists("yep"))
}

func TestStoreLoadNotFound(t *testing.T) {
	s := tempStore(t)

	_, err := s.Load("nonexistent")
	assert.Error(t, err)
}

func TestStoreConfigPath(t *testing.T) {
	s := NewStore("/tmp/.claude")
	path := s.configPath("my-team")
	assert.Equal(t, filepath.Join("/tmp/.claude", "teams", "my-team", "config.json"), path)
}
