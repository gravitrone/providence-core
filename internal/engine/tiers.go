package engine

import "strings"

// --- Model Tiers ---

// ModelTier groups models by relative speed and capability.
type ModelTier int

const (
	// TierFast is the lowest-latency tier.
	TierFast ModelTier = iota
	// TierMedium balances speed and capability.
	TierMedium
	// TierCapable is the highest-capability tier.
	TierCapable
)

// String returns the display label for the model tier.
func (t ModelTier) String() string {
	switch t {
	case TierFast:
		return "fast"
	case TierMedium:
		return "medium"
	case TierCapable:
		return "capable"
	default:
		return "unknown"
	}
}

// --- Model Catalog ---

// ModelSpec describes a supported model entry.
type ModelSpec struct {
	Name            string
	Aliases         []string
	Provider        string // "anthropic" | "openai" | "openrouter"
	Tier            ModelTier
	ContextWindow   int
	MaxOutputTokens int
	Display         string
}

// ModelCatalog is the provider-agnostic catalog of supported models.
var ModelCatalog = []ModelSpec{
	{Name: "claude-haiku-4-5-20251001", Aliases: []string{"haiku"}, Provider: "anthropic", Tier: TierFast, ContextWindow: 200000, MaxOutputTokens: 8192, Display: "claude-haiku-4-5"},
	{Name: "claude-sonnet-4-6", Aliases: []string{"sonnet"}, Provider: "anthropic", Tier: TierMedium, ContextWindow: 200000, MaxOutputTokens: 8192, Display: "claude-sonnet-4-6"},
	{Name: "claude-opus-4-6", Aliases: []string{"opus"}, Provider: "anthropic", Tier: TierCapable, ContextWindow: 200000, MaxOutputTokens: 8192, Display: "claude-opus-4-6"},
	{Name: "gpt-5.4-mini", Aliases: []string{"codex-mini", "gpt5-mini"}, Provider: "openai", Tier: TierFast, ContextWindow: 200000, MaxOutputTokens: 16384, Display: "gpt-5.4-mini"},
	{Name: "gpt-5.2", Aliases: []string{"gpt5.2"}, Provider: "openai", Tier: TierMedium, ContextWindow: 200000, MaxOutputTokens: 16384, Display: "gpt-5.2"},
	{Name: "gpt-5.4", Aliases: []string{"codex", "gpt5"}, Provider: "openai", Tier: TierCapable, ContextWindow: 200000, MaxOutputTokens: 16384, Display: "gpt-5.4"},
	{Name: "gpt-5.3-codex", Aliases: []string{"codex-5.3"}, Provider: "openai", Tier: TierMedium, ContextWindow: 200000, MaxOutputTokens: 16384, Display: "gpt-5.3-codex"},
	{Name: "gpt-5.2-codex", Aliases: []string{"codex-5.2"}, Provider: "openai", Tier: TierMedium, ContextWindow: 200000, MaxOutputTokens: 16384, Display: "gpt-5.2-codex"},
	{Name: "gpt-5.1-codex-max", Aliases: []string{"codex-max", "codex-5.1-max"}, Provider: "openai", Tier: TierCapable, ContextWindow: 200000, MaxOutputTokens: 16384, Display: "gpt-5.1-codex-max"},
	{Name: "gpt-5.1-codex-mini", Aliases: []string{"codex-5.1", "codex-5.1-mini"}, Provider: "openai", Tier: TierFast, ContextWindow: 200000, MaxOutputTokens: 16384, Display: "gpt-5.1-codex-mini"},
	{Name: "anthropic/claude-sonnet-4-5", Aliases: []string{"or-sonnet"}, Provider: "openrouter", Tier: TierMedium, ContextWindow: 200000, MaxOutputTokens: 8192, Display: "anthropic/claude-sonnet-4-5"},
	{Name: "openai/gpt-5.4", Aliases: []string{"or-gpt5"}, Provider: "openrouter", Tier: TierCapable, ContextWindow: 200000, MaxOutputTokens: 16384, Display: "openai/gpt-5.4"},
	{Name: "google/gemini-2.5-pro", Aliases: []string{"or-gemini"}, Provider: "openrouter", Tier: TierMedium, ContextWindow: 1000000, MaxOutputTokens: 8192, Display: "google/gemini-2.5-pro"},
	{Name: "deepseek/deepseek-chat", Aliases: []string{"or-deepseek"}, Provider: "openrouter", Tier: TierFast, ContextWindow: 64000, MaxOutputTokens: 8192, Display: "deepseek/deepseek-chat"},
	{Name: "meta-llama/llama-3.3-70b-instruct", Aliases: []string{"or-llama"}, Provider: "openrouter", Tier: TierFast, ContextWindow: 128000, MaxOutputTokens: 8192, Display: "meta-llama/llama-3.3-70b-instruct"},
}

var (
	modelSpecsByLookup = map[string]*ModelSpec{}
	fastModelsBySource = map[string]string{}
)

func init() {
	for i := range ModelCatalog {
		spec := &ModelCatalog[i]
		modelSpecsByLookup[lookupModelKey(spec.Name)] = spec
		for _, alias := range spec.Aliases {
			modelSpecsByLookup[lookupModelKey(alias)] = spec
		}
		provider := lookupModelKey(spec.Provider)
		if spec.Tier == TierFast && fastModelsBySource[provider] == "" {
			fastModelsBySource[provider] = spec.Name
		}
	}
}

// --- Lookup Helpers ---

// SpecFor returns the model specification for a canonical name or alias.
func SpecFor(model string) *ModelSpec {
	return modelSpecsByLookup[lookupModelKey(model)]
}

// TierFor returns the model tier for a canonical name or alias.
func TierFor(model string) ModelTier {
	spec := SpecFor(model)
	if spec == nil {
		return TierMedium
	}
	return spec.Tier
}

// ContextWindowFor returns the model context window for a canonical name or alias.
func ContextWindowFor(model string) int {
	spec := SpecFor(model)
	if spec == nil {
		return 200000
	}
	return spec.ContextWindow
}

// MaxOutputTokensFor returns the max output tokens for a canonical name or alias.
func MaxOutputTokensFor(model string) int {
	spec := SpecFor(model)
	if spec == nil || spec.MaxOutputTokens <= 0 {
		return 8192
	}
	return spec.MaxOutputTokens
}

// FastForProvider returns the first fast-tier model for the given provider.
func FastForProvider(provider string) string {
	return fastModelsBySource[lookupModelKey(provider)]
}

// ResolveAlias resolves a canonical name or alias to the canonical model name.
func ResolveAlias(input string) string {
	spec := SpecFor(input)
	if spec == nil {
		return input
	}
	return spec.Name
}

func lookupModelKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
