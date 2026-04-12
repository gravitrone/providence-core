package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// execHTTPHook POSTs the hook input as JSON to the configured URL.
func execHTTPHook(ctx context.Context, cfg HookConfig, input HookInput) (*HookOutput, error) {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal hook input: %w", err)
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = time.Duration(ToolHookTimeoutMS) * time.Millisecond
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(inputJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create hook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute hook request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read hook response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("hook returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	return parseOutput(body), nil
}
