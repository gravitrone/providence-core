# Providence Core

The Profaned Core. Autonomous AI harness wrapping Claude Code in a flame-themed TUI.

## Critical Rules

- ALWAYS use the flame theme from `styles.go` (ColorPrimary #FFA600, ColorSecondary #D77757).
- ALWAYS use Providence-themed naming (profaned, flame, ember, consecrate, etc).
- ALWAYS run `make test && make lint` before any commit.
- NEVER add co-author tags to commits.
- NEVER commit without explicit ask.

## Architecture

Go TUI at repo root (`cmd/`, `internal/`), web UI planned in `web/src/` (Next.js).

- Engine interface (`internal/engine/engine.go`) abstracts AI backends. Claude engine in `internal/engine/claude/`.
- System prompt (`internal/engine/prompt.go`) is shared across all engines.
- `AgentTab` is a standalone struct (not `tea.Model`) - the single view, no tabs.
- Banner scrolls inside the viewport as chat fills.
- `flameColor(frame)` sine wave drives all animations.
- Message queue: enter = queue, shift+enter = steer (priority). All steered messages combine into one on drain.

### Engine Layer

Wraps Claude Code headless via NDJSON streaming (`--input-format stream-json --output-format stream-json`). Custom tools injectable via `--mcp-config`, `mcp_set_servers` stdin, or `.mcp.json`.

The `Interrupt()` method on Engine sends SIGINT without killing the session. Currently unused - reserved for future direct API engine with mid-turn steering.

## Stack Decisions (Locked)

- **Bubble Tea v2** + **Lip Gloss v2** + **Glamour v2** + **Harmonica** for TUI
- **Cobra** for CLI
- **golangci-lint** for linting, pre-commit hooks mandatory

## Commands

```
make build        # build ./providence binary
make test         # go test -race -count=1
make lint         # golangci-lint
make install-bin  # install to /usr/local/bin
make setup        # full setup from fresh clone
```

## Implementation Pitfalls

- `lipgloss.Color` is a FUNCTION in v2, not a type. Use `string` for hex values, wrap with `lipgloss.Color()` at render time.
- Bubble Tea v2 sends escape as `"esc"` not `"escape"`.
- `AgentTab.handleKey` is a value receiver. Mutations go through `prepareSend` (pointer receiver). Don't mix.
- `randomToolFlavor()` must be called once at message creation, not per render tick.
- `centerBlockUniform` uses `lipgloss.Width` - trim trailing spaces from ASCII art or centering drifts.
- After `Cancel()`, session is nil. Always nil-check via `safeWaitForEvent()`.

## Commit Style

Conventional commits: `feat|fix|refactor|docs|infra|test|chore(scope): description`

## Compact Instructions

Always preserve: current task, file paths being edited, test results, architectural decisions, steering/queue behavior, engine interface contract.

## Do NOT

- Use long dashes - use "-" or commas
- Add AI slop ("furthermore", "it's worth noting")
- Import external project packages - copy and adapt if needed
- Reference vault paths or personal files in repo code
- Skip pre-commit hooks with --no-verify
