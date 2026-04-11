# Providence Core

The Profaned Core. Autonomous AI harness wrapping Claude Code, OpenAI Codex, and OpenRouter in a flame-themed TUI.

This file is the canonical project rules for BOTH Claude Code and OpenAI Codex CLI. Read it before making any changes.

## Critical Rules

- ALWAYS use the flame theme from `internal/ui/styles.go` (ColorPrimary #FFA600, ColorSecondary #D77757).
- ALWAYS use Providence-themed naming (profaned, flame, ember, consecrate, etc).
- ALWAYS run `make test && make lint` before any commit.
- NEVER add co-author tags to commits.
- NEVER commit without explicit ask.
- NEVER commit directly to main - use feature branches (`feat/<name>`, `fix/<name>`).

## Architecture

Monorepo: `cmd/providence/` (entrypoint) + `internal/` (all packages). Web UI planned in `web/src/` (Next.js).

- Engine interface (`internal/engine/engine.go`) abstracts AI backends with factory registration
- Claude headless engine (`internal/engine/claude/`) wraps `claude -p` subprocess via NDJSON protocol
- Direct engine (`internal/engine/direct/`) calls Anthropic Messages API with own agent loop
- Direct engine also supports Codex (OAuth to ChatGPT) and OpenRouter (300+ models) as sub-modes
- System prompt (`internal/engine/prompt.go`) is shared across all engines
- Tools implemented in Go at `internal/engine/direct/tools/` (Read, Write, Edit, Bash, Glob, Grep, WebFetch, WebSearch)
- `AgentTab` is a standalone struct (not `tea.Model`) - the single view, no tabs
- Banner scrolls inside the viewport as chat fills
- `flameColor(frame)` sine wave drives all animations
- Message queue: enter = queue, shift+enter = steer (priority). Steered messages combine into one on drain
- Viz system renders 12 chart/table types from fenced `providence-viz` code blocks
- Sessions persist to SQLite at `~/.providence/sessions.db`
- Config at `~/.providence/config.json` (engine, model, theme)
- OpenAI OAuth tokens at `~/.providence/openai-auth.json`

### Engine Layer

- Claude headless: wraps Claude Code via NDJSON streaming (`--input-format stream-json --output-format stream-json`). Custom tools via `--mcp-config`, `mcp_set_servers` stdin, or `.mcp.json`
- Direct: own agent loop using `github.com/anthropics/anthropic-sdk-go`. Streaming tool queue (read-only parallel, write serial). Mid-turn steering support
- Codex: OAuth via `auth.openai.com` PKCE flow, hits `chatgpt.com/backend-api/codex/responses`
- OpenRouter: `OPENROUTER_API_KEY` env var, OpenAI-compatible Chat Completions format

The `Interrupt()` method on Engine sends SIGINT without killing the session. Reserved for future mid-turn steering work.

### Session Persistence

- SQLite database at `~/.providence/sessions.db` (modernc.org/sqlite, pure Go)
- Sessions table: id, cwd, engine_type, model, title, timestamps, token_count, cost_usd
- Messages table: full ChatMessage fields with cascade delete
- `/sessions` lists past sessions for current CWD
- `/resume N` restores messages + rebuilds engine history (tool history rendered as text blocks)

## Stack Decisions (Locked)

- **Bubble Tea v2** + **Lip Gloss v2** + **Glamour v2** + **Harmonica** for TUI
- **Cobra** for CLI
- **modernc.org/sqlite** for session storage (pure Go, no CGO)
- **golangci-lint** for linting, pre-commit hooks mandatory

## Go Conventions

- Standard goimports grouping: stdlib, external, local (blank line separators)
- Section separators: `// --- Section Name ---` with proper capitalization
- Exported functions MUST have doc comments starting with function name
- Unexported helpers get doc comments if sibling helpers have them (consistency)
- All API types use `json` struct tags
- Error messages lowercase: `fmt.Errorf("failed to create entity: %w", err)`
- NEVER panic - return errors with `%w` wrapping
- Bubble Tea: Model-Update-View pattern. Messages are typed structs, not strings

## Testing Conventions

- Every test MUST assert something meaningful. No assertion-free View() calls
- NEVER use NotPanics as the sole assertion. Test actual state/output
- PREFER table-driven/parametrized tests over copy-paste test functions
- Use `require` for fatal checks, `assert` for non-fatal
- Test names describe the scenario: `TestRestoreHistory_WithTools`, `TestDetailCommandRequiresOneArg`
- Use `t.TempDir()` for filesystem tests to avoid pollution
- Tests that need HTTP should use `httptest.NewServer`, never real network

## Implementation Pitfalls

- `lipgloss.Color` is a FUNCTION in v2, not a type. Use `string` for hex values, wrap with `lipgloss.Color()` at render time
- Bubble Tea v2 sends escape as `"esc"` not `"escape"`
- `AgentTab.handleKey` is a value receiver. Mutations go through `prepareSend` (pointer receiver). Don't mix
- `randomToolFlavor()` must be called once at message creation, not per render tick
- `centerBlockUniform` uses `lipgloss.Width` - trim trailing spaces from ASCII art or centering drifts
- After `Cancel()`, session is nil. Always nil-check via `safeWaitForEvent()`
- Tool persistence only on `Done=true` - streaming messages persist when finalized
- `/resume` uses synthetic text format `[Tool: Name(args) -> output]` for tool history (no tool_use_id pairing)

## Commands

```
make build        # build ./providence binary
make test         # go test -race -count=1
make lint         # golangci-lint
make install-bin  # install to /usr/local/bin
make setup        # full setup from fresh clone
```

## Branch Discipline

- Feature branches: `feat/<name>`
- Bug fixes: `fix/<name>`
- Work on branch, merge to main after local verification
- Never force push to main
- Tag releases after merge: `git tag v0.X.Y && git push origin v0.X.Y`

## Commit Style

Conventional commits: `feat|fix|refactor|docs|infra|test|chore(scope): description`

- Subject line imperative mood, under 72 chars
- Body explains the "why" for non-trivial changes
- Reference related SPEC.md sections when implementing vision items

## Compact Instructions

Always preserve: current task, file paths being edited, test results, architectural decisions, steering/queue behavior, engine interface contract, active branch name.

## Do NOT

- Use long dashes - use "-" or commas
- Add AI slop ("furthermore", "it's worth noting", "comprehensive")
- Import external project packages - copy and adapt if needed
- Reference vault paths or personal files in repo code
- Skip pre-commit hooks with --no-verify
- Commit `providence` binary, `.env`, or anything in `.gitignore`
- Modify `SPEC.md` without explicit user ask (it's the vision doc)
</content>
</invoke>