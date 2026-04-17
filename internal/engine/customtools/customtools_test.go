package customtools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseToolManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool.yaml")

	content := `name: greeter
description: Greets a user by name
inputs:
  name:
    type: string
    description: The name to greet
    required: true
  loud:
    type: boolean
    description: Whether to shout
    required: false
command: "echo Hello {{name}}"
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	m, err := ParseToolManifest(path)
	require.NoError(t, err)

	assert.Equal(t, "greeter", m.Name)
	assert.Equal(t, "Greets a user by name", m.Description)
	assert.Len(t, m.Inputs, 2)
	assert.Equal(t, "string", m.Inputs["name"].Type)
	assert.True(t, m.Inputs["name"].Required)
	assert.False(t, m.Inputs["loud"].Required)
	assert.Equal(t, "echo Hello {{name}}", m.Command)
	assert.Empty(t, m.Stdin)
}

func TestCustomToolCallStdin(t *testing.T) {
	dir := t.TempDir()
	tool := &CustomTool{
		Manifest: ToolManifest{
			Name:    "json-echo",
			Command: "cat",
			Stdin:   "json",
		},
		Dir: dir,
	}

	input := json.RawMessage(`{"key":"value","num":42}`)
	result, err := tool.Call(context.Background(), input)
	require.NoError(t, err)
	assert.JSONEq(t, `{"key":"value","num":42}`, result)
}

func TestCustomToolCallTemplate(t *testing.T) {
	dir := t.TempDir()
	tool := &CustomTool{
		Manifest: ToolManifest{
			Name:    "greeter",
			Command: "echo Hello {{name}}",
		},
		Dir: dir,
	}

	input := json.RawMessage(`{"name":"providence"}`)
	result, err := tool.Call(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, "Hello providence\n", result)
}

func TestCustomToolTimeout(t *testing.T) {
	dir := t.TempDir()
	tool := &CustomTool{
		Manifest: ToolManifest{
			Name:    "sleeper",
			Command: "sleep 30",
		},
		Dir: dir,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := tool.Call(ctx, nil)
	require.Error(t, err)
	// The parent context times out before the tool's internal 120s timeout,
	// so the command gets killed either way.
	assert.Contains(t, err.Error(), "sleeper")
}

func TestLoadCustomToolsFromDir(t *testing.T) {
	projectDir := t.TempDir()
	homeDir := t.TempDir()

	// Create project tool.
	toolDir := filepath.Join(projectDir, ".providence", "tools", "lister")
	require.NoError(t, os.MkdirAll(toolDir, 0o755))
	manifest := `name: lister
description: Lists files
command: "ls"
`
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, "tool.yaml"), []byte(manifest), 0o644))

	// Create second project tool.
	toolDir2 := filepath.Join(projectDir, ".providence", "tools", "counter")
	require.NoError(t, os.MkdirAll(toolDir2, 0o755))
	manifest2 := `name: counter
description: Counts things
command: "wc -l"
stdin: json
`
	require.NoError(t, os.WriteFile(filepath.Join(toolDir2, "tool.yaml"), []byte(manifest2), 0o644))

	tools, err := LoadCustomTools(projectDir, homeDir)
	require.NoError(t, err)
	assert.Len(t, tools, 2)

	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Manifest.Name] = true
	}
	assert.True(t, names["lister"])
	assert.True(t, names["counter"])
}

// TestCustomToolRejectsShellInjection exercises the argv substitution
// contract. The fixed tool writes $1 to a sentinel file verbatim; any
// shell-metacharacter value must end up in that file as a single string,
// not trigger a spawned shell command. If the old "sh -c" primitive
// regressed, the injection would run, the sentinel would either be
// missing or contain the shell-expanded output, and this test would fail.
func TestCustomToolRejectsShellInjection(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "sentinel.txt")

	// A minimal script that writes $1 to a file. Using a script (not a
	// shell one-liner) ensures no shell interpretation happens on the
	// Go side; any injection would manifest by the script never being
	// invoked with the expected argv.
	scriptPath := filepath.Join(dir, "capture.sh")
	script := "#!/bin/sh\nprintf '%s' \"$1\" > \"" + sentinel + "\"\n"
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	injections := []struct {
		name     string
		payload  string
	}{
		{"semicolon", `foo; rm -rf ~`},
		{"command-substitution", `$(rm -rf ~)`},
		{"backtick", "`rm -rf ~`"},
		{"pipe", `foo | cat /etc/passwd`},
		{"logical-and", `foo && echo pwned`},
	}

	for _, inj := range injections {
		inj := inj
		t.Run(inj.name, func(t *testing.T) {
			// Remove any prior sentinel so a stale file can't mask a failure.
			_ = os.Remove(sentinel)

			tool := &CustomTool{
				Manifest: ToolManifest{
					Name:    "capture",
					Command: scriptPath + " {{value}}",
				},
				Dir: dir,
			}

			payload, err := json.Marshal(map[string]string{"value": inj.payload})
			require.NoError(t, err)

			_, err = tool.Call(context.Background(), payload)
			require.NoError(t, err, "tool should execute with the injection payload passed as a single argv element")

			got, err := os.ReadFile(sentinel)
			require.NoError(t, err, "sentinel must exist: the script should have been invoked with the payload as $1")
			assert.Equal(t, inj.payload, string(got),
				"injection payload must land verbatim in $1, proving no shell interpreted it")
		})
	}
}

// TestCustomToolRejectsEmbeddedPlaceholder verifies the parser refuses
// templates where a placeholder is concatenated with other characters
// inside the same token. Allowing these would require reintroducing shell
// interpretation to reassemble the token, which is exactly the injection
// primitive we removed.
func TestCustomToolRejectsEmbeddedPlaceholder(t *testing.T) {
	tool := &CustomTool{
		Manifest: ToolManifest{
			Name:    "bad-template",
			Command: "grep {{pattern}}-suffix file.txt",
		},
		Dir: t.TempDir(),
	}

	input := json.RawMessage(`{"pattern":"foo"}`)
	_, err := tool.Call(context.Background(), input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "embedded")
}

// TestCustomToolRejectsUnterminatedQuote pins the splitter's quote
// handling: a dangling quote must surface as a template error rather than
// silently concatenating the remainder into a single token.
func TestCustomToolRejectsUnterminatedQuote(t *testing.T) {
	tool := &CustomTool{
		Manifest: ToolManifest{
			Name:    "bad-quote",
			Command: `echo "unterminated`,
		},
		Dir: t.TempDir(),
	}

	_, err := tool.Call(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unterminated quote")
}

// TestCustomToolEnvSubstitution verifies inputs are exposed as $P_<KEY>
// env vars so tools that prefer env access over positional argv still
// work without shell interpretation at substitution time.
func TestCustomToolEnvSubstitution(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "env.txt")

	scriptPath := filepath.Join(dir, "env-capture.sh")
	script := "#!/bin/sh\nprintf '%s' \"$P_NAME\" > \"" + sentinel + "\"\n"
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	tool := &CustomTool{
		Manifest: ToolManifest{
			Name:    "env-capture",
			Command: scriptPath,
		},
		Dir: dir,
	}

	// A dangerous value again; proves env substitution is also injection-safe.
	payload, err := json.Marshal(map[string]string{"name": `; rm -rf ~`})
	require.NoError(t, err)

	_, err = tool.Call(context.Background(), payload)
	require.NoError(t, err)

	got, err := os.ReadFile(sentinel)
	require.NoError(t, err)
	assert.Equal(t, `; rm -rf ~`, string(got))
}

// TestBuildArgvTable is a table-driven check on the splitter + substitution
// pipeline in isolation, covering happy paths and several error paths so
// regressions in buildArgv surface with targeted diagnostics.
func TestBuildArgvTable(t *testing.T) {
	cases := []struct {
		name     string
		template string
		params   map[string]any
		wantArgv []string
		wantErr  string
	}{
		{
			name:     "simple",
			template: "echo hello",
			wantArgv: []string{"echo", "hello"},
		},
		{
			name:     "placeholder-substitution",
			template: "echo {{msg}}",
			params:   map[string]any{"msg": "world"},
			wantArgv: []string{"echo", "world"},
		},
		{
			name:     "quoted-literal-preserved",
			template: `echo "hello world"`,
			wantArgv: []string{"echo", "hello world"},
		},
		{
			name:     "missing-placeholder-rejected",
			template: "echo {{msg}}",
			params:   map[string]any{},
			wantErr:  "missing input",
		},
		{
			name:     "embedded-placeholder-rejected",
			template: "echo {{msg}}-tail",
			params:   map[string]any{"msg": "x"},
			wantErr:  "embedded",
		},
		{
			name:     "empty-template-rejected",
			template: "",
			wantErr:  "empty command template",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			argv, err := buildArgv(tc.template, tc.params)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantArgv, argv)
		})
	}
}

// TestCustomToolTimeoutArgvPath keeps the pre-existing timeout contract
// alive after the sh -c removal: "sleep 30" must still be parsed as argv
// and cancelled by the parent context. Without this test a regression
// that lost CommandContext wiring would only surface in production.
func TestCustomToolTimeoutArgvPath(t *testing.T) {
	tool := &CustomTool{
		Manifest: ToolManifest{
			Name:    "timeout-check",
			Command: "sleep 30",
		},
		Dir: t.TempDir(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := tool.Call(ctx, nil)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.True(t, elapsed < 5*time.Second,
		fmt.Sprintf("timeout must fire promptly, took %s", elapsed))
	// sanity: name must appear somewhere in the error for diagnostics
	assert.True(t, strings.Contains(err.Error(), "timeout-check"),
		"error should identify the tool name")
}
