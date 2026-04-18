package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gravitrone/providence-core/internal/engine/hooks"
)

const (
	defaultTimeoutMs  = 120_000
	maxTimeoutMs      = 600_000
	cwdSentinelPrefix = "__PVD_CWD__="
)

// BashTool executes shell commands via /bin/bash.
type BashTool struct {
	SandboxDisabled bool // skip sandbox-exec wrapping

	mu          sync.Mutex
	sessionID   string
	sessionRoot string
	cwdFile     string
	emitter     HookEmitter
}

func NewBashTool() *BashTool { return &BashTool{} }

// SetHookEmitter wires lifecycle hook dispatch for bash session updates.
func (b *BashTool) SetHookEmitter(emitter HookEmitter) {
	b.emitter = emitter
}

// Close removes the persisted cwd state for this bash session.
func (b *BashTool) Close() error {
	path := b.cwdStatePath()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove bash cwd state: %w", err)
	}
	return nil
}

// sandboxProfile is a macOS sandbox-exec profile that blocks network access
// and writes to /System while allowing everything else.
const sandboxProfile = `(version 1)(allow default)(deny network*)(deny file-write* (subpath "/System"))`

func (b *BashTool) Name() string        { return "Bash" }
func (b *BashTool) Description() string { return "Execute a bash command and return its output." }
func (b *BashTool) ReadOnly() bool      { return false }

// Prompt implements ToolPrompter with CC-parity guidance for bash execution.
func (b *BashTool) Prompt() string {
	return `Executes a given bash command and returns its output.

The working directory persists between commands, but shell state does not. The shell environment is initialized from the user's profile (bash or zsh).

IMPORTANT: Avoid using this tool to run ` + "`find`, `grep`, `cat`, `head`, `tail`, `sed`, `awk`, or `echo`" + ` commands, unless explicitly instructed or after you have verified that a dedicated tool cannot accomplish your task. Instead, use the appropriate dedicated tool as this will provide a much better experience for the user:

 - File search: Use Glob (NOT find or ls)
 - Content search: Use Grep (NOT grep or rg)
 - Read files: Use Read (NOT cat/head/tail)
 - Edit files: Use Edit (NOT sed/awk)
 - Write files: Use Write (NOT echo >/cat <<EOF)
 - Communication: Output text directly (NOT echo/printf)
While the Bash tool can do similar things, it's better to use the built-in tools as they provide a better user experience and make it easier to review tool calls and give permission.

# Instructions
 - If your command will create new directories or files, first use this tool to run ` + "`ls`" + ` to verify the parent directory exists and is the correct location.
 - Always quote file paths that contain spaces with double quotes in your command (e.g., cd "path with spaces/file.txt")
 - Try to maintain your current working directory throughout the session by using absolute paths and avoiding usage of ` + "`cd`" + `. You may use ` + "`cd`" + ` if the User explicitly requests it.
 - You may specify an optional timeout in milliseconds (up to 600000ms / 10 minutes). By default, your command will timeout after 120000ms (2 minutes).
 - You can use the ` + "`run_in_background`" + ` parameter to run the command in the background. Only use this if you don't need the result immediately and are OK being notified when the command completes later. You do not need to check the output right away - you'll be notified when it finishes. You do not need to use '&' at the end of the command when using this parameter.
 - When issuing multiple commands:
   - If the commands are independent and can run in parallel, make multiple Bash tool calls in a single message.
   - If the commands depend on each other and must run sequentially, use a single Bash call with '&&' to chain them together.
   - Use ';' only when you need to run commands sequentially but don't care if earlier commands fail.
   - DO NOT use newlines to separate commands (newlines are ok in quoted strings).
 - For git commands:
   - Prefer to create a new commit rather than amending an existing commit.
   - Before running destructive operations (e.g., git reset --hard, git push --force, git checkout --), consider whether there is a safer alternative that achieves the same goal. Only use destructive operations when they are truly the best approach.
   - Never skip hooks (--no-verify) or bypass signing (--no-gpg-sign, -c commit.gpgsign=false) unless the user has explicitly asked for it. If a hook fails, investigate and fix the underlying issue.
 - Avoid unnecessary ` + "`sleep`" + ` commands:
   - Do not sleep between commands that can run immediately - just run them.
   - If your command is long running and you would like to be notified when it finishes - use ` + "`run_in_background`" + `. No sleep needed.
   - Do not retry failing commands in a sleep loop - diagnose the root cause.
   - If waiting for a background task you started with ` + "`run_in_background`" + `, you will be notified when it completes - do not poll.

## Command sandbox
By default on macOS, your command runs in a sandbox that blocks network access and writes to /System. If a command fails due to sandbox restrictions, retry with ` + "`dangerously_disable_sandbox: true`" + `.
For temporary files, use the $TMPDIR environment variable. Do NOT use /tmp directly - use $TMPDIR instead.

# Committing changes with git

Only create commits when requested by the user. If unclear, ask first. When the user asks you to create a new git commit, follow these steps carefully:

Git Safety Protocol:
- NEVER update the git config
- NEVER run destructive git commands (push --force, reset --hard, checkout ., restore ., clean -f, branch -D) unless the user explicitly requests these actions
- NEVER skip hooks (--no-verify, --no-gpg-sign, etc) unless the user explicitly requests it
- NEVER run force push to main/master, warn the user if they request it
- CRITICAL: Always create NEW commits rather than amending, unless the user explicitly requests a git amend. When a pre-commit hook fails, the commit did NOT happen - so --amend would modify the PREVIOUS commit. Fix the issue, re-stage, and create a NEW commit
- When staging files, prefer adding specific files by name rather than using "git add -A" or "git add .", which can accidentally include sensitive files (.env, credentials) or large binaries
- NEVER commit changes unless the user explicitly asks you to

1. Run the following bash commands in parallel, each using the Bash tool:
  - Run a git status command to see all untracked files. IMPORTANT: Never use the -uall flag as it can cause memory issues on large repos.
  - Run a git diff command to see both staged and unstaged changes that will be committed.
  - Run a git log command to see recent commit messages, so that you can follow this repository's commit message style.
2. Analyze all staged changes (both previously staged and newly added) and draft a commit message:
  - Summarize the nature of the changes (eg. new feature, enhancement, bug fix, refactoring, test, docs, etc.).
  - Do not commit files that likely contain secrets (.env, credentials.json, etc). Warn the user if they specifically request to commit those files.
  - Draft a concise (1-2 sentences) commit message that focuses on the "why" rather than the "what".
3. Run the following commands in parallel:
   - Add relevant untracked files to the staging area.
   - Create the commit with the message.
   - Run git status after the commit completes to verify success.
4. If the commit fails due to pre-commit hook: fix the issue and create a NEW commit.

Important notes:
- NEVER run additional commands to read or explore code, besides git bash commands
- NEVER use the TodoWrite or Agent tools during commits
- DO NOT push to the remote repository unless the user explicitly asks you to do so
- IMPORTANT: Never use git commands with the -i flag (like git rebase -i or git add -i) since they require interactive input which is not supported.
- If there are no changes to commit (no untracked files and no modifications), do not create an empty commit.
- ALWAYS pass the commit message via a HEREDOC for proper formatting.

# Creating pull requests
Use the gh command via the Bash tool for ALL GitHub-related tasks including working with issues, pull requests, checks, and releases.

When the user asks you to create a pull request:
1. Run git status, git diff, git log, and git diff [base-branch]...HEAD in parallel to understand the full commit history.
2. Analyze all changes that will be included in the PR (ALL commits, not just the latest), draft a title (<70 chars) and summary.
3. Push to remote with -u flag if needed, create PR using gh pr create.
Return the PR URL when done.

# Other common operations
- View comments on a Github PR: gh api repos/foo/bar/pulls/123/comments`
}

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

	// Security check before execution.
	if check := CheckBashSecurity(command); !check.Allowed {
		return ToolResult{
			Content: fmt.Sprintf("command blocked by security check: %s", check.Reason),
			IsError: true,
		}
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
	wrappedCommand, err := b.commandWithStartDir(command, false)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}

	cmd := b.makeCmd(ctx, wrappedCommand, sandbox)
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

	wrappedCommand, err := b.commandWithStartDir(command, true)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}

	cmd := b.makeCmd(ctx, wrappedCommand, sandbox)
	// Kill entire process group on timeout.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err = cmd.Run()

	output := buf.String()
	output, updatedCwd := stripCwdSentinel(output)

	// Spill oversized output to disk so the model gets a head+tail
	// preview plus a path pointer rather than a silently-truncated
	// body. The spill threshold lives in resultstore.go.
	shortOutput, spillPath := SpillIfLarge(b.sessionKey(), "Bash", output)

	if ctx.Err() == context.DeadlineExceeded {
		msg := fmt.Sprintf("Command timed out after %s\n\n%s", timeout, shortOutput)
		return ToolResult{Content: msg, IsError: true, Metadata: spillMetadata(spillPath)}
	}

	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return ToolResult{Content: fmt.Sprintf("failed to run command: %v", err), IsError: true}
		}
	}

	if exitCode == 0 && updatedCwd != "" {
		if saveErr := b.saveCwd(updatedCwd); saveErr != nil {
			return ToolResult{
				Content: fmt.Sprintf("save bash cwd state: %v", saveErr),
				IsError: true,
			}
		}
		b.emitCwdChanged(updatedCwd)
	}

	result := shortOutput + "\n\nExit code: " + strconv.Itoa(exitCode)

	meta := map[string]any{"exit_code": exitCode}
	if spillPath != "" {
		meta["spill_path"] = spillPath
	}
	return ToolResult{
		Content:  result,
		IsError:  exitCode != 0,
		Metadata: meta,
	}
}

func (b *BashTool) emitCwdChanged(dir string) {
	if b.emitter == nil {
		return
	}
	b.emitter(hooks.CwdChanged, hooks.HookInput{
		ToolName: b.Name(),
		ToolInput: map[string]string{
			"cwd": dir,
		},
	})
}

func (b *BashTool) commandWithStartDir(command string, captureCwd bool) (string, error) {
	startDir, err := b.resolveStartDir()
	if err != nil {
		return "", fmt.Errorf("resolve bash start dir: %w", err)
	}

	wrapped := "cd " + shellQuote(startDir) + " && " + command
	if !captureCwd {
		return wrapped, nil
	}

	return wrapped + "\npvd_status=$?\nif [ $pvd_status -eq 0 ]; then printf '\\n" + cwdSentinelPrefix + "%s\\n' \"$PWD\"; fi\nexit $pvd_status", nil
}

func (b *BashTool) resolveStartDir() (string, error) {
	root, err := b.resolveSessionRoot()
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(b.cwdStatePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return root, nil
		}
		return "", fmt.Errorf("read bash cwd state: %w", err)
	}

	savedDir := strings.TrimSpace(string(data))
	if savedDir == "" {
		return root, nil
	}
	if valid, _ := isExistingDir(savedDir); valid {
		return savedDir, nil
	}

	return root, nil
}

func (b *BashTool) resolveSessionRoot() (string, error) {
	b.mu.Lock()
	root := b.sessionRoot
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			b.mu.Unlock()
			return "", fmt.Errorf("get working directory: %w", err)
		}
		b.sessionRoot = cwd
		root = cwd
	}
	b.mu.Unlock()

	valid, err := isExistingDir(root)
	if err != nil {
		return "", fmt.Errorf("stat session root %q: %w", root, err)
	}
	if !valid {
		return "", fmt.Errorf("session root %q is not a directory", root)
	}

	return root, nil
}

func (b *BashTool) saveCwd(dir string) error {
	if valid, err := isExistingDir(dir); err != nil {
		return fmt.Errorf("stat cwd %q: %w", dir, err)
	} else if !valid {
		return fmt.Errorf("cwd %q is not a directory", dir)
	}

	if err := os.WriteFile(b.cwdStatePath(), []byte(dir+"\n"), 0o600); err != nil {
		return fmt.Errorf("write bash cwd state: %w", err)
	}
	return nil
}

func (b *BashTool) cwdStatePath() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.sessionID == "" {
		b.sessionID = "session-" + randTag(6)
	}
	if b.cwdFile == "" {
		b.cwdFile = filepath.Join(os.TempDir(), fmt.Sprintf("providence-bash-%s-cwd", sanitiseForPath(b.sessionID)))
	}

	return b.cwdFile
}

func (b *BashTool) sessionKey() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.sessionID == "" {
		b.sessionID = "session-" + randTag(6)
	}

	return b.sessionID
}

func isExistingDir(path string) (bool, error) {
	//nolint:gosec
	// Bash cwd paths come from the session root or prior successful pwd output.
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func stripCwdSentinel(output string) (string, string) {
	idx := strings.LastIndex(output, "\n"+cwdSentinelPrefix)
	if idx == -1 {
		return output, ""
	}

	valueStart := idx + 1 + len(cwdSentinelPrefix)
	valueEnd := strings.IndexByte(output[valueStart:], '\n')
	if valueEnd == -1 {
		return output, ""
	}
	valueEnd += valueStart

	return output[:idx] + output[valueEnd+1:], output[valueStart:valueEnd]
}

// spillMetadata returns a Metadata map carrying the spill path when
// one exists, nil otherwise. Keeps the timeout branch concise.
func spillMetadata(spillPath string) map[string]any {
	if spillPath == "" {
		return nil
	}
	return map[string]any{"spill_path": spillPath}
}
