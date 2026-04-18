package tools

import (
	"context"
	"sync"
	"testing"

	"github.com/gravitrone/providence-core/internal/engine/hooks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubTool is a minimal Tool for testing the registry.
type stubTool struct {
	name     string
	readOnly bool
}

type promptStubTool struct {
	stubTool
	prompt string
}

type hookSpy struct {
	mu     sync.Mutex
	events []string
	inputs []hooks.HookInput
}

func (s *hookSpy) record(event string, input hooks.HookInput) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	s.inputs = append(s.inputs, input)
}

func (s *hookSpy) snapshot() ([]string, []hooks.HookInput) {
	s.mu.Lock()
	defer s.mu.Unlock()

	events := append([]string(nil), s.events...)
	inputs := append([]hooks.HookInput(nil), s.inputs...)
	return events, inputs
}

func (s *stubTool) Name() string                                           { return s.name }
func (s *stubTool) Description() string                                    { return "stub" }
func (s *stubTool) InputSchema() map[string]any                            { return nil }
func (s *stubTool) ReadOnly() bool                                         { return s.readOnly }
func (s *stubTool) Execute(_ context.Context, _ map[string]any) ToolResult { return ToolResult{} }
func (s *promptStubTool) Prompt() string                                   { return s.prompt }

func TestRegistryGetAndAll(t *testing.T) {
	a := &stubTool{name: "alpha", readOnly: true}
	b := &stubTool{name: "beta", readOnly: false}

	reg := NewRegistry(a, b)

	got, ok := reg.Get("alpha")
	require.True(t, ok)
	assert.Equal(t, "alpha", got.Name())

	_, ok = reg.Get("missing")
	assert.False(t, ok)

	all := reg.All()
	assert.Len(t, all, 2)
}

func TestRegistryEmpty(t *testing.T) {
	reg := NewRegistry()
	_, ok := reg.Get("anything")
	assert.False(t, ok)
	assert.Empty(t, reg.All())
}

func TestRegistryOverwrite(t *testing.T) {
	a1 := &stubTool{name: "dup", readOnly: true}
	a2 := &stubTool{name: "dup", readOnly: false}

	reg := NewRegistry(a1, a2)
	got, ok := reg.Get("dup")
	require.True(t, ok)
	// last one wins
	assert.False(t, got.ReadOnly())
}

func TestParamHelpers(t *testing.T) {
	input := map[string]any{
		"str":      "hello",
		"num_f64":  float64(42),
		"num_int":  100,
		"bad_type": true,
	}

	assert.Equal(t, "hello", paramString(input, "str", ""))
	assert.Equal(t, "default", paramString(input, "missing", "default"))
	assert.Equal(t, "default", paramString(input, "bad_type", "default"))

	assert.Equal(t, 42, paramInt(input, "num_f64", 0))
	assert.Equal(t, 100, paramInt(input, "num_int", 0))
	assert.Equal(t, 99, paramInt(input, "missing", 99))
	assert.Equal(t, 99, paramInt(input, "bad_type", 99))
}

func TestCollectToolPromptsUnwrapsRegistryTools(t *testing.T) {
	reg := NewRegistry(&promptStubTool{
		stubTool: stubTool{name: "prompted", readOnly: true},
		prompt:   "use the prompted tool carefully",
	})

	prompts := CollectToolPrompts(reg)
	assert.Contains(t, prompts, "## prompted")
	assert.Contains(t, prompts, "use the prompted tool carefully")
}
