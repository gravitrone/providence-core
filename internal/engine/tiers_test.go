package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelCatalogValid(t *testing.T) {
	t.Parallel()

	allowedProviders := map[string]struct{}{
		"anthropic":  {},
		"openai":     {},
		"openrouter": {},
	}

	for _, spec := range ModelCatalog {
		spec := spec
		t.Run(spec.Name, func(t *testing.T) {
			t.Parallel()

			require.NotEmpty(t, spec.Name)
			_, ok := allowedProviders[spec.Provider]
			assert.True(t, ok, "provider must be supported")
			assert.Greater(t, spec.ContextWindow, 0)
		})
	}
}

func TestSpecForKnown(t *testing.T) {
	t.Parallel()

	for _, spec := range ModelCatalog {
		spec := spec
		t.Run(spec.Name, func(t *testing.T) {
			t.Parallel()

			got := SpecFor(spec.Name)
			require.NotNil(t, got)
			assert.Equal(t, spec.Name, got.Name)
		})
	}
}

func TestSpecForUnknown(t *testing.T) {
	t.Parallel()

	assert.Nil(t, SpecFor("fake-model"))
}

func TestResolveAlias(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "haiku alias", input: "haiku", want: "claude-haiku-4-5-20251001"},
		{name: "codex alias", input: "codex", want: "gpt-5.4"},
		{name: "canonical anthropic", input: "claude-sonnet-4-6", want: "claude-sonnet-4-6"},
		{name: "canonical openai", input: "gpt-5.4", want: "gpt-5.4"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ResolveAlias(tt.input))
		})
	}
}

func TestFastForProviderEachProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		provider   string
		want       string
		wantFilled bool
	}{
		{name: "anthropic", provider: "anthropic", want: "claude-haiku-4-5-20251001"},
		{name: "openai", provider: "openai", want: "gpt-5.4-mini"},
		{name: "openrouter", provider: "openrouter", wantFilled: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := FastForProvider(tt.provider)
			if tt.wantFilled {
				assert.NotEmpty(t, got)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFastForProviderUnknown(t *testing.T) {
	t.Parallel()

	assert.Empty(t, FastForProvider("fake-provider"))
}

func TestContextWindowFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		model string
		want  int
	}{
		{name: "anthropic", model: "claude-haiku-4-5-20251001", want: 200000},
		{name: "openrouter gemini", model: "google/gemini-2.5-pro", want: 1000000},
		{name: "unknown", model: "fake-model", want: 200000},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ContextWindowFor(tt.model))
		})
	}
}

func TestTierForEach(t *testing.T) {
	t.Parallel()

	tiers := []struct {
		name string
		tier ModelTier
	}{
		{name: "fast", tier: TierFast},
		{name: "medium", tier: TierMedium},
		{name: "capable", tier: TierCapable},
	}

	seen := map[ModelTier]bool{}
	for _, spec := range ModelCatalog {
		seen[TierFor(spec.Name)] = true
	}

	for _, tt := range tiers {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.True(t, seen[tt.tier], "expected at least one model for tier %s", tt.tier.String())
		})
	}
}
