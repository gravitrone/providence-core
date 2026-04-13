---
name: code-conventions
description: Go code style conventions for Providence Core
---

# Code Conventions (Go)

## Imports
- goimports grouping: stdlib, external, local with blank line separators
- Example:
  ```go
  import (
      "fmt"
      "strings"

      "github.com/charmbracelet/lipgloss/v2"

      "github.com/gravitrone/providence-core/internal/engine"
  )
  ```

## Section Separators
- Use `// --- Section Name ---` with proper capitalization
- One blank line before and after

## Comments
- Standard `//` only, proper capitalization
- Exported functions MUST have doc comments starting with function name
- Only comment non-obvious logic
- NO `/* */` block comments for sections
- NO decorative patterns (=====, #####, ****)

## Error Handling
- Return errors, don't panic
- Wrap with `fmt.Errorf("context: %w", err)`

## Struct Tags
- Always `json` tags on API/config types

## Commits
- Conventional format: type(scope): description
- NO co-author tags
