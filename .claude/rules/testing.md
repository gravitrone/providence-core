---
paths: ["internal/**/*_test.go", "cmd/**/*_test.go"]
---

# Testing Conventions

- Every test MUST assert something meaningful. No assertion-free View() calls.
- NEVER use NotPanics as the sole assertion. Test actual state/output.
- PREFER table-driven/parametrized tests over copy-paste test functions.
- Go: use `require` for fatal checks, `assert` for non-fatal.
- Test names describe the scenario: `TestDetailCommandRequiresOneArg`
- Use `t.TempDir()` for filesystem tests to avoid pollution
- Tests that need HTTP should use `httptest.NewServer`, never real network
