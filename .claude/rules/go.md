---
paths: ["internal/**/*.go", "cmd/**/*.go"]
---

# Go Conventions

- Standard goimports grouping: stdlib, external, local (blank line separators)
- Section separators: `// --- Section Name ---` with proper capitalization
- Exported functions MUST have doc comments starting with function name
- Doc comments use third-person present tense verbs: creates, returns, handles, updates
- Inline comments: lowercase first letter, above the line, not end of line
- All API types use `json` struct tags
- Error messages lowercase: `fmt.Errorf("failed to create entity: %w", err)`
- Error messages prefix with operation context: `"read config: %w"`, not just `"error: %w"`
- Wrap errors with `%w`, never `%v` for wrapped errors
- Wrap at call site once, propagate bare errors up from API/DB layers
- Use `errors.Is()` for sentinel checks (`os.ErrNotExist`, `syscall.EPERM`, etc)
- Custom error types implement `Error() string` with nil-safe receivers
- Validation errors include concrete context (line numbers, expected/actual values)
- NEVER panic. Return errors with `%w` wrapping.
- NO em/en dashes (U+2014, U+2013) anywhere in code, comments, strings, or docs
- Bubble Tea: Model-Update-View pattern. Messages are types, not strings.
