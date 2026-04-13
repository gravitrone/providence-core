package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

const (
	defaultTimeoutMs = 120_000
	maxTimeoutMs     = 600_000
	maxOutputLen     = 30_000
)

// BashTool executes shell commands via /bin/bash.
type BashTool struct {
	SandboxDisabled bool // skip sandbox-exec wrapping
}

func NewBashTool() *BashTool { return &BashTool{} }

// sandboxProfile is a macOS sandbox-exec profile that blocks network access
// and writes to /System while allowing everything else.
const sandboxProfile = `(version 1)(allow default)(deny network*)(deny file-write* (subpath "/System"))`

func (b *BashTool) Name() string        { return "Bash" }
func (b *BashTool) Description() string { return "Execute a bash command and return its output." }
func (b *BashTool) ReadOnly() bool      { return false }

func (b *BashTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The bash command to execute.",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Timeout in milliseconds (default 120000, max 600000).",
			},
			"run_in_background": map[string]any{
				"type":        "boolean",
				"description": "Start the command in the background and return its PID.",
			},
			"dangerously_disable_sandbox": map[string]any{
				"type":        "boolean",
				"description": "Disable macOS sandbox-exec wrapping for this command.",
			},
		},
		"required": []string{"command"},
	}
}

func (b *BashTool) Execute(ctx context.Context, input map[string]any) ToolResult {
	command := paramString(input, "command", "")
	if command == "" {
		return ToolResult{Content: "command is required", IsError: true}
	}

	timeoutMs := paramInt(input, "timeout", defaultTimeoutMs)
	if timeoutMs <= 0 {
		timeoutMs = defaultTimeoutMs
	}
	if timeoutMs > maxTimeoutMs {
		timeoutMs = maxTimeoutMs
	}

	background := paramBool(input, "run_in_background", false)
	disableSandbox := paramBool(input, "dangerously_disable_sandbox", false)
	useSandbox := runtime.GOOS == "darwin" && !b.SandboxDisabled && !disableSandbox

	if background {
		return b.runBackground(ctx, command, useSandbox)
	}
	return b.runForeground(ctx, command, time.Duration(timeoutMs)*time.Millisecond, useSandbox)
}

func (b *BashTool) makeCmd(ctx context.Context, command string, sandbox bool) *exec.Cmd {
	if sandbox {
		return exec.CommandContext(ctx, "sandbox-exec", "-p", sandboxProfile, "/bin/bash", "-c", command)
	}
	return exec.CommandContext(ctx, "/bin/bash", "-c", command)
}

func (b *BashTool) runBackground(ctx context.Context, command string, sandbox bool) ToolResult {
	cmd := b.makeCmd(ctx, command, sandbox)
	// Detach process group so it survives parent exit.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to start: %v", err), IsError: true}
	}

	pid := cmd.Process.Pid
	// Release the process so we don't wait on it.
	go cmd.Wait() //nolint:errcheck

	return ToolResult{
		Content:  fmt.Sprintf("Started background process with PID %d", pid),
		Metadata: map[string]any{"pid": pid},
	}
}

func (b *BashTool) runForeground(ctx context.Context, command string, timeout time.Duration, sandbox bool) ToolResult {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := b.makeCmd(ctx, command, sandbox)
	// Kill entire process group on timeout.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()

	output := buf.String()
	truncated := false
	if len(output) > maxOutputLen {
		output = output[:maxOutputLen]
		truncated = true
	}

	if ctx.Err() == context.DeadlineExceeded {
		msg := fmt.Sprintf("Command timed out after %s\n\n%s", timeout, output)
		if truncated {
			msg += "\n\n[output truncated]"
		}
		return ToolResult{Content: msg, IsError: true}
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return ToolResult{Content: fmt.Sprintf("failed to run command: %v", err), IsError: true}
		}
	}

	result := output
	if truncated {
		result += "\n\n[output truncated]"
	}
	result += "\n\nExit code: " + strconv.Itoa(exitCode)

	return ToolResult{
		Content:  result,
		IsError:  exitCode != 0,
		Metadata: map[string]any{"exit_code": exitCode},
	}
}
