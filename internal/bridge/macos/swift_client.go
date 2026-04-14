//go:build darwin

package macos

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var swiftRequestID atomic.Uint64

type clickParams struct {
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Button string `json:"button,omitempty"`
	Count  int    `json:"count,omitempty"`
}

type typeTextParams struct {
	Text string `json:"text"`
}

type keyComboParams struct {
	Key         string   `json:"key"`
	Modifiers   []string `json:"modifiers,omitempty"`
	VirtualCode int      `json:"virtual_code"`
}

type swiftClient struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	scanner   *bufio.Scanner
	pending   map[string]chan Response
	mu        sync.Mutex
	pendingMu sync.Mutex
	events    chan Event
	done      chan struct{}
	spawnErr  error
}

type swiftEnvelope struct {
	ID     string          `json:"id,omitempty"`
	OK     bool            `json:"ok,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ProtocolError  `json:"error,omitempty"`
	Event  string          `json:"event,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

func (c *swiftClient) Click(ctx context.Context, params clickParams) error {
	return c.callAction(ctx, "click", params)
}

func (c *swiftClient) TypeText(ctx context.Context, text string) error {
	return c.callAction(ctx, "type_text", typeTextParams{Text: text})
}

// AXTree requests the Accessibility tree for the given app/PID.
func (c *swiftClient) AXTree(ctx context.Context, p AXTreeParams) (AXTreeResult, error) {
	raw, err := c.call(ctx, "ax_tree", p)
	if err != nil {
		return AXTreeResult{}, err
	}
	var result AXTreeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return AXTreeResult{}, fmt.Errorf("ax_tree: decode result: %w", err)
	}
	return result, nil
}

// AXFind searches for elements matching the given query.
func (c *swiftClient) AXFind(ctx context.Context, q AXQuery) (AXFindResult, error) {
	raw, err := c.call(ctx, "ax_find", q)
	if err != nil {
		return AXFindResult{}, err
	}
	var result AXFindResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return AXFindResult{}, fmt.Errorf("ax_find: decode result: %w", err)
	}
	return result, nil
}

// AXPerform triggers an accessibility action on an element.
func (c *swiftClient) AXPerform(ctx context.Context, elementID, action string) error {
	params := AXPerformParams{ElementID: elementID, Action: action}
	return c.callAction(ctx, "ax_perform", params)
}

func (c *swiftClient) KeyCombo(ctx context.Context, combo KeyCombo) error {
	return c.callAction(ctx, "key_combo", keyComboParams{
		Key:         combo.Key,
		Modifiers:   combo.Modifiers,
		VirtualCode: combo.VirtualCode,
	})
}

func spawnSwift(ctx context.Context, binary string, timeout time.Duration) (*swiftClient, error) {
	if binary == "" {
		return nil, fmt.Errorf("swift bridge binary is empty")
	}

	readyTimeout := timeout
	if readyTimeout == 0 {
		readyTimeout = 1500 * time.Millisecond
	}

	cmd := exec.CommandContext(ctx, binary)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start swift bridge: %w", err)
	}

	client := &swiftClient{
		cmd:     cmd,
		stdin:   stdin,
		scanner: bufio.NewScanner(stdout),
		pending: make(map[string]chan Response),
		events:  make(chan Event, 16),
		done:    make(chan struct{}),
	}
	client.scanner.Buffer(make([]byte, 16<<20), 16<<20)

	go func() {
		_, _ = io.Copy(io.Discard, stderr)
	}()

	ready := make(chan error, 1)
	go client.readLoop(ready)

	timer := time.NewTimer(readyTimeout)
	defer timer.Stop()

	select {
	case err := <-ready:
		if err != nil {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return nil, err
		}
		return client, nil
	case <-timer.C:
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil, fmt.Errorf("swift bridge ready timeout after %s", readyTimeout)
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil, ctx.Err()
	case <-client.done:
		return nil, client.currentSpawnError()
	}
}

func (c *swiftClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	requestID := fmt.Sprintf("%d", swiftRequestID.Add(1))
	responseCh := make(chan Response, 1)

	c.pendingMu.Lock()
	c.pending[requestID] = responseCh
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, requestID)
		c.pendingMu.Unlock()
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if err := c.writeRequest(Request{
		ID:     requestID,
		Method: method,
		Params: params,
	}); err != nil {
		return nil, err
	}

	select {
	case resp := <-responseCh:
		if !resp.OK {
			if resp.Error != nil {
				return nil, resp.Error
			}
			return nil, &ProtocolError{
				Code:    ErrInternal,
				Message: "swift bridge request failed",
			}
		}
		return resp.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, c.currentSpawnError()
	}
}

// Events returns the unsolicited event stream from the Swift bridge.
func (c *swiftClient) Events() <-chan Event {
	return c.events
}

// Close shuts down the Swift bridge subprocess.
func (c *swiftClient) Close(ctx context.Context) error {
	if c == nil || c.cmd == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()

	type result struct {
		err error
	}

	writeDone := make(chan result, 1)
	go func() {
		writeDone <- result{err: c.writeRequest(Request{
			ID:     fmt.Sprintf("shutdown-%d", swiftRequestID.Add(1)),
			Method: "shutdown",
		})}
	}()

	select {
	case res := <-writeDone:
		if res.err != nil {
			return res.err
		}
	case <-shutdownCtx.Done():
		return shutdownCtx.Err()
	}

	if c.waitForDone(ctx, 500*time.Millisecond) {
		return nil
	}

	if c.cmd.Process != nil {
		_ = c.cmd.Process.Signal(syscall.SIGTERM)
	}
	if c.waitForDone(ctx, 500*time.Millisecond) {
		return nil
	}

	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}

	if c.waitForDone(ctx, 500*time.Millisecond) {
		return nil
	}

	return nil
}

// --- Internal Helpers ---

func (c *swiftClient) writeRequest(req Request) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal swift request: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := c.stdin.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("write swift request: %w", err)
	}

	return nil
}

func (c *swiftClient) readLoop(ready chan<- error) {
	readySent := false

	notifyReady := func(err error) {
		if readySent {
			return
		}
		readySent = true
		if ready != nil {
			ready <- err
		}
	}

	for c.scanner.Scan() {
		var envelope swiftEnvelope
		if err := json.Unmarshal(c.scanner.Bytes(), &envelope); err != nil {
			notifyReady(fmt.Errorf("decode swift message: %w", err))
			c.fail(fmt.Errorf("decode swift message: %w", err))
			return
		}

		if envelope.ID != "" {
			response := Response{
				ID:     envelope.ID,
				OK:     envelope.OK,
				Result: envelope.Result,
				Error:  envelope.Error,
			}
			c.pendingMu.Lock()
			responseCh := c.pending[envelope.ID]
			c.pendingMu.Unlock()
			if responseCh != nil {
				responseCh <- response
			}
			continue
		}

		if envelope.Event == "" {
			continue
		}

		if envelope.Event == "ready" {
			notifyReady(nil)
			continue
		}

		event := Event{
			Type: envelope.Event,
			Data: envelope.Data,
		}
		c.events <- event
	}

	if err := c.scanner.Err(); err != nil {
		notifyReady(fmt.Errorf("read swift bridge output: %w", err))
		c.fail(fmt.Errorf("read swift bridge output: %w", err))
		return
	}

	exitErr := fmt.Errorf("swift bridge exited")
	notifyReady(exitErr)
	c.fail(exitErr)
}

func (c *swiftClient) fail(err error) {
	c.mu.Lock()
	c.spawnErr = err
	c.mu.Unlock()

	c.pendingMu.Lock()
	for id, responseCh := range c.pending {
		responseCh <- Response{
			ID: id,
			OK: false,
			Error: &ProtocolError{
				Code:    ErrInternal,
				Message: err.Error(),
			},
		}
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()

	close(c.done)
	close(c.events)
}

func (c *swiftClient) currentSpawnError() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.spawnErr == nil {
		return fmt.Errorf("swift bridge exited")
	}

	return c.spawnErr
}

func (c *swiftClient) waitForDone(ctx context.Context, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-c.done:
		return true
	case <-ctx.Done():
		return false
	case <-timer.C:
		return false
	}
}

func (c *swiftClient) callAction(ctx context.Context, method string, params any) error {
	payload, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal %s params: %w", method, err)
	}

	result, err := c.call(ctx, method, json.RawMessage(payload))
	if err != nil {
		return err
	}

	return decodeActionResult(result)
}

func decodeActionResult(result json.RawMessage) error {
	trimmed := bytes.TrimSpace(result)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}

	var payload struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return fmt.Errorf("failed to decode swift action result: %w", err)
	}
	if payload.OK {
		return nil
	}

	return fmt.Errorf("swift action did not return ok")
}
