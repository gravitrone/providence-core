package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// --- Initialize handshake parsing ---

func TestInitializeResponseParsing(t *testing.T) {
	raw := `{
		"capabilities": {"tools": {}},
		"serverInfo": {"name": "test-server", "version": "0.1.0"},
		"instructions": "Use this server for file operations."
	}`

	var initResult struct {
		Instructions string `json:"instructions"`
	}
	err := json.Unmarshal([]byte(raw), &initResult)
	assert.NoError(t, err)
	assert.Equal(t, "Use this server for file operations.", initResult.Instructions)
}

func TestInitializeResponseNoInstructions(t *testing.T) {
	raw := `{
		"capabilities": {},
		"serverInfo": {"name": "minimal", "version": "0.1.0"}
	}`

	var initResult struct {
		Instructions string `json:"instructions"`
	}
	err := json.Unmarshal([]byte(raw), &initResult)
	assert.NoError(t, err)
	assert.Equal(t, "", initResult.Instructions)
}

// --- ListTools response parsing ---

func TestListToolsResponseParsing(t *testing.T) {
	raw := `{
		"tools": [
			{
				"name": "read_file",
				"description": "Read a file",
				"inputSchema": {"type": "object", "properties": {"path": {"type": "string"}}}
			},
			{
				"name": "write_file",
				"description": "Write a file",
				"inputSchema": {"type": "object", "properties": {"path": {"type": "string"}, "content": {"type": "string"}}}
			}
		]
	}`

	var result struct {
		Tools []ToolDef `json:"tools"`
	}
	err := json.Unmarshal([]byte(raw), &result)
	require.NoError(t, err)
	assert.Len(t, result.Tools, 2)
	assert.Equal(t, "read_file", result.Tools[0].Name)
	assert.Equal(t, "write_file", result.Tools[1].Name)
	assert.Equal(t, "object", result.Tools[0].InputSchema["type"])
}

func TestListToolsResponseEmpty(t *testing.T) {
	raw := `{"tools": []}`

	var result struct {
		Tools []ToolDef `json:"tools"`
	}
	err := json.Unmarshal([]byte(raw), &result)
	require.NoError(t, err)
	assert.Empty(t, result.Tools)
}

// --- CallTool result parsing ---

func TestCallToolResultParsing(t *testing.T) {
	raw := `{
		"content": [
			{"type": "text", "text": "file contents here"}
		],
		"isError": false
	}`

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	err := json.Unmarshal([]byte(raw), &result)
	require.NoError(t, err)
	assert.Len(t, result.Content, 1)
	assert.Equal(t, "text", result.Content[0].Type)
	assert.Equal(t, "file contents here", result.Content[0].Text)
	assert.False(t, result.IsError)
}

func TestCallToolResultMultipleBlocks(t *testing.T) {
	raw := `{
		"content": [
			{"type": "text", "text": "first block"},
			{"type": "image", "data": "base64stuff"},
			{"type": "text", "text": "second block"}
		],
		"isError": false
	}`

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &result))

	// Simulate CallTool concatenation: only text blocks.
	var text string
	for _, block := range result.Content {
		if block.Type == "text" {
			if text != "" {
				text += "\n"
			}
			text += block.Text
		}
	}
	assert.Equal(t, "first block\nsecond block", text)
}

func TestCallToolResultError(t *testing.T) {
	raw := `{
		"content": [
			{"type": "text", "text": "file not found"}
		],
		"isError": true
	}`

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &result))
	assert.True(t, result.IsError)
	assert.Equal(t, "file not found", result.Content[0].Text)
}

// --- Invalid JSON handling ---

func TestJSONRPCResponseInvalidJSON(t *testing.T) {
	raw := `this is not json at all`
	var resp jsonrpcResponse
	err := json.Unmarshal([]byte(raw), &resp)
	assert.Error(t, err)
}

func TestJSONRPCResponsePartialJSON(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1`
	var resp jsonrpcResponse
	err := json.Unmarshal([]byte(raw), &resp)
	assert.Error(t, err)
}

func TestToolDefInvalidJSON(t *testing.T) {
	raw := `{"name": 123}` // name should be string
	var def ToolDef
	err := json.Unmarshal([]byte(raw), &def)
	assert.Error(t, err)
}

func TestCallToolResultInvalidJSON(t *testing.T) {
	raw := `{bad json`
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	err := json.Unmarshal([]byte(raw), &result)
	assert.Error(t, err)
}

// --- Connection error / scanner behavior ---

func TestScannerEmptyStream(t *testing.T) {
	// Simulates what happens when server closes connection (empty reader).
	reader := strings.NewReader("")
	scanner := bufio.NewScanner(reader)

	scanned := scanner.Scan()
	assert.False(t, scanned, "empty stream should not yield any scan")
	assert.NoError(t, scanner.Err())
}

func TestScannerSkipsMalformedLines(t *testing.T) {
	// Simulates the client's call() loop behavior: skip lines that aren't valid JSONRPC.
	lines := "not json\n\n{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n"
	scanner := bufio.NewScanner(strings.NewReader(lines))

	var found *jsonrpcResponse
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var resp jsonrpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		if resp.ID != 0 || resp.Error != nil || resp.Result != nil {
			found = &resp
			break
		}
	}

	require.NotNil(t, found)
	assert.Equal(t, int64(1), found.ID)
	assert.NotNil(t, found.Result)
}

func TestScannerSkipsNotifications(t *testing.T) {
	// Notifications have no id. The client's call() loop should skip them.
	lines := strings.Join([]string{
		`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progress":50}}`,
		`{"jsonrpc":"2.0","id":5,"result":{"data":"hello"}}`,
	}, "\n") + "\n"
	scanner := bufio.NewScanner(strings.NewReader(lines))

	targetID := int64(5)
	var found *jsonrpcResponse
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var resp jsonrpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		if resp.ID == 0 && resp.Error == nil && resp.Result == nil {
			continue // skip notification
		}
		if resp.ID == targetID {
			found = &resp
			break
		}
	}

	require.NotNil(t, found)
	assert.Equal(t, int64(5), found.ID)
}

// --- Notification marshaling ---

func TestNotificationMarshalNoID(t *testing.T) {
	type notification struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}

	n := notification{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	data, err := json.Marshal(n)
	require.NoError(t, err)

	// Must not contain an "id" field.
	assert.NotContains(t, string(data), `"id"`)
	assert.Contains(t, string(data), `"method":"notifications/initialized"`)
}

// --- Request ID incrementing ---

func TestClientNextIDIncrementsAtomically(t *testing.T) {
	c := &Client{}
	id1 := c.nextID.Add(1)
	id2 := c.nextID.Add(1)
	id3 := c.nextID.Add(1)
	assert.Equal(t, int64(1), id1)
	assert.Equal(t, int64(2), id2)
	assert.Equal(t, int64(3), id3)
}

// --- Request wire format ---

func TestRequestWireFormat(t *testing.T) {
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      42,
		Method:  "tools/call",
		Params: map[string]any{
			"name":      "read_file",
			"arguments": map[string]any{"path": "/tmp/test.txt"},
		},
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)
	data = append(data, '\n')

	// Must be a single line terminated by newline.
	assert.Equal(t, 1, bytes.Count(data, []byte("\n")))

	// Round-trip: read from scanner.
	scanner := bufio.NewScanner(bytes.NewReader(data))
	assert.True(t, scanner.Scan())

	var parsed jsonrpcRequest
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &parsed))
	assert.Equal(t, "2.0", parsed.JSONRPC)
	assert.Equal(t, int64(42), parsed.ID)
	assert.Equal(t, "tools/call", parsed.Method)
}

// --- RPC error response ---

func TestRPCErrorResponseCode(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":3,"error":{"code":-32600,"message":"Invalid Request"}}`

	var resp jsonrpcResponse
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32600, resp.Error.Code)
	assert.Equal(t, "Invalid Request", resp.Error.Message)
	assert.Nil(t, resp.Result)
}

