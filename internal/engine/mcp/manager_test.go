package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager(t *testing.T) {
	mgr := NewManager()
	require.NotNil(t, mgr)
	assert.Equal(t, 0, mgr.ServerCount())
}

func TestManagerGetAllToolsEmpty(t *testing.T) {
	mgr := NewManager()
	tools := mgr.GetAllTools()
	assert.Empty(t, tools)
}

func TestManagerGetInstructionsEmpty(t *testing.T) {
	mgr := NewManager()
	assert.Equal(t, "", mgr.GetInstructions())
}

func TestManagerCallToolNotConnected(t *testing.T) {
	mgr := NewManager()
	_, err := mgr.CallTool("nonexistent", "tool", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestManagerCloseAllEmpty(t *testing.T) {
	mgr := NewManager()
	mgr.CloseAll() // should not panic
	assert.Equal(t, 0, mgr.ServerCount())
}

func TestManagerConnectAllSkipsNonStdio(t *testing.T) {
	mgr := NewManager()
	err := mgr.ConnectAll([]ServerConfig{
		{Name: "sse-only", Type: "sse", Command: "fake"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, mgr.ServerCount())
}
