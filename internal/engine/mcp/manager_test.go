package mcp

import (
	"strings"
	"testing"
	"time"

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

func TestManagerAppliesInstructionDelta(t *testing.T) {
	mgr := NewManager()
	client, _ := newTestClient("")
	client.instructions = "initial instructions"

	mgr.clients["filesystem"] = client
	mgr.instructionCache["filesystem"] = client.GetInstructions()

	mgr.handleNotification("filesystem", "notifications/instructions/update", []byte(`{"instructions":"updated instructions"}`))

	assert.Contains(t, mgr.GetInstructions(), "updated instructions")

	attachments := mgr.TakeTurnAttachments()
	require.Len(t, attachments, 1)
	assert.Equal(t, "filesystem", attachments[0].ServerName)
	assert.Contains(t, attachments[0].Content, "updated instructions")
	assert.Empty(t, mgr.TakeTurnAttachments())
}

func TestManagerRefreshesToolsOnNotification(t *testing.T) {
	client, writer := newTestClient(strings.Join([]string{
		`{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`,
		`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"old","description":"old tool","inputSchema":{"type":"object"}}]}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"old","description":"old tool","inputSchema":{"type":"object"}},{"name":"new","description":"new tool","inputSchema":{"type":"object"}}]}}`,
	}, "\n") + "\n")

	mgr := NewManager()
	mgr.clients["filesystem"] = client
	mgr.bindNotificationHandler("filesystem", client)

	mgr.RefreshTools()

	require.Eventually(t, func() bool {
		tools := mgr.GetAllTools()["filesystem"]
		if len(tools) != 2 {
			return false
		}
		return tools[0].Name == "old" && tools[1].Name == "new"
	}, time.Second, 10*time.Millisecond)

	assert.Equal(t, 2, strings.Count(writer.String(), `"method":"tools/list"`))
}
