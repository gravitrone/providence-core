package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJSONRPCRequestMarshal(t *testing.T) {
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2024-11-05",
		},
	}

	data, err := json.Marshal(req)
	assert.NoError(t, err)

	var parsed map[string]any
	assert.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "2.0", parsed["jsonrpc"])
	assert.Equal(t, float64(1), parsed["id"])
	assert.Equal(t, "initialize", parsed["method"])
}

func TestJSONRPCResponseUnmarshal(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"result":{"capabilities":{},"serverInfo":{"name":"test","version":"0.1"}}}`

	var resp jsonrpcResponse
	err := json.Unmarshal([]byte(raw), &resp)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), resp.ID)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)
}

func TestJSONRPCResponseError(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"Method not found"}}`

	var resp jsonrpcResponse
	err := json.Unmarshal([]byte(raw), &resp)
	assert.NoError(t, err)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, -32601, resp.Error.Code)
	assert.Equal(t, "Method not found", resp.Error.Message)
}

func TestToolDefUnmarshal(t *testing.T) {
	raw := `{
		"name": "read_file",
		"description": "Read file contents",
		"inputSchema": {
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "File path"}
			},
			"required": ["path"]
		}
	}`

	var def ToolDef
	err := json.Unmarshal([]byte(raw), &def)
	assert.NoError(t, err)
	assert.Equal(t, "read_file", def.Name)
	assert.Equal(t, "Read file contents", def.Description)
	assert.Equal(t, "object", def.InputSchema["type"])
}
