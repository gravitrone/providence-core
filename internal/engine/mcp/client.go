package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
)

// ToolDef is the tool definition returned by an MCP server.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Client manages a single MCP server connection over stdio JSON-RPC 2.0.
type Client struct {
	name         string
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       *bufio.Scanner
	tools        []ToolDef
	instructions string

	mu    sync.Mutex
	nextID atomic.Int64
}

// jsonrpcRequest is a JSON-RPC 2.0 request.
type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonrpcResponse is a JSON-RPC 2.0 response.
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewStdioClient spawns an MCP server subprocess and returns a connected Client.
func NewStdioClient(cfg ServerConfig) (*Client, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)

	// Merge current env with config env overrides.
	env := os.Environ()
	for k, v := range cfg.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	cmd.Stderr = os.Stderr // let server stderr pass through for debugging

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", cfg.Command, err)
	}

	c := &Client{
		name:   cfg.Name,
		cmd:    cmd,
		stdin:  stdinPipe,
		stdout: bufio.NewScanner(stdoutPipe),
	}
	// Set a reasonable max scan buffer (10 MB) for large tool responses.
	c.stdout.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	return c, nil
}

// Initialize performs the MCP initialize handshake with the server.
func (c *Client) Initialize() error {
	type initParams struct {
		ProtocolVersion string         `json:"protocolVersion"`
		Capabilities    map[string]any `json:"capabilities"`
		ClientInfo      map[string]any `json:"clientInfo"`
	}

	resp, err := c.call("initialize", initParams{
		ProtocolVersion: "2024-11-05",
		Capabilities:    map[string]any{},
		ClientInfo: map[string]any{
			"name":    "providence",
			"version": "1.0.0",
		},
	})
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	// Extract server instructions if provided.
	var initResult struct {
		Instructions string `json:"instructions"`
	}
	if err := json.Unmarshal(resp, &initResult); err == nil && initResult.Instructions != "" {
		c.instructions = initResult.Instructions
	}

	// Send initialized notification (no response expected).
	return c.notify("notifications/initialized", nil)
}

// ListTools calls tools/list and stores the result.
func (c *Client) ListTools() ([]ToolDef, error) {
	resp, err := c.call("tools/list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}

	var result struct {
		Tools []ToolDef `json:"tools"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse tools/list: %w", err)
	}

	c.tools = result.Tools
	return result.Tools, nil
}

// CallTool invokes a tool on the MCP server and returns the text result.
func (c *Client) CallTool(name string, args map[string]any) (string, error) {
	type callParams struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}

	resp, err := c.call("tools/call", callParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return "", fmt.Errorf("tools/call %s: %w", name, err)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("parse tools/call result: %w", err)
	}

	// Concatenate all text content blocks.
	var text string
	for _, block := range result.Content {
		if block.Type == "text" {
			if text != "" {
				text += "\n"
			}
			text += block.Text
		}
	}

	if result.IsError {
		return text, fmt.Errorf("MCP tool error: %s", text)
	}
	return text, nil
}

// GetInstructions returns the server-provided instructions from initialization.
func (c *Client) GetInstructions() string {
	return c.instructions
}

// Close shuts down the MCP server subprocess.
func (c *Client) Close() error {
	c.stdin.Close()
	return c.cmd.Wait()
}

// call sends a JSON-RPC request and reads the response.
func (c *Client) call(method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID.Add(1)
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')

	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Read lines until we get a response matching our ID.
	// Skip notifications (messages without an id field).
	for {
		if !c.stdout.Scan() {
			if err := c.stdout.Err(); err != nil {
				return nil, fmt.Errorf("read response: %w", err)
			}
			return nil, fmt.Errorf("server closed connection")
		}

		line := c.stdout.Bytes()
		if len(line) == 0 {
			continue
		}

		var resp jsonrpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // skip malformed lines
		}

		// Skip notifications (id == 0 means the field was absent or zero).
		if resp.ID == 0 && resp.Error == nil && resp.Result == nil {
			continue
		}

		if resp.ID != id {
			continue // not our response, skip
		}

		if resp.Error != nil {
			return nil, fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// notify sends a JSON-RPC notification (no response expected).
func (c *Client) notify(method string, params any) error {
	// Notifications have no id field - use a struct without ID.
	type notification struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}

	data, err := json.Marshal(notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}
	data = append(data, '\n')

	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.stdin.Write(data)
	return err
}
