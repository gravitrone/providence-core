package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// execShellHook runs a shell command with JSON on stdin and as an env var.
// Exit code semantics: 0 = success, 2 = blocking error, other = non-blocking error.
func execShellHook(ctx context.Context, cfg HookConfig, input HookInput) (*HookOutput, error) {
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

	cmd := exec.CommandContext(ctx, "sh", "-c", cfg.Command)
	cmd.Stdin = bytes.NewReader(inputJSON)
	cmd.Env = append(cmd.Environ(), "CLAUDE_HOOK_INPUT="+string(inputJSON))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("hook timed out after %s: %s", timeout, cfg.Command)
	}

	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			return nil, fmt.Errorf("failed to run hook command: %w", err)
		}

		exitCode := exitErr.ExitCode()
		if exitCode == 2 {
			// Blocking error - parse output if available, return BlockingError
			out := parseOutput(stdout.Bytes())
			msg := stderr.String()
			if msg == "" {
				msg = "hook returned blocking error"
			}
			return out, &BlockingError{Message: msg, Output: out}
		}

		// Non-blocking error - log but don't block
		return nil, fmt.Errorf("hook exited with code %d: %s", exitCode, stderr.String())
	}

	return parseOutput(stdout.Bytes()), nil
}

// parseOutput attempts to parse bytes as HookOutput JSON.
// Returns nil for empty or unparseable output (empty = success).
func parseOutput(data []byte) *HookOutput {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil
	}

	var out HookOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return &out
}
