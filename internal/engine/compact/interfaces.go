package compact

import "context"

// Provider defines the provider-specific hooks needed by the compaction
// orchestrator.
type Provider interface {
	// Compress allows a provider to perform compaction directly. Returning zero
	// means the orchestrator should fall back to Serialize -> OneShot -> Replace.
	Compress(ctx context.Context, keepRecentTokens int) (int, error)
	// Serialize renders the prefix that should be compacted and returns the cut
	// index where the recent tail begins.
	Serialize(keepRecentTokens int) (string, int, error)
	// Replace swaps the compacted prefix with a single replacement message while
	// preserving the recent tail starting at cutIndex.
	Replace(summary string, cutIndex int) error
	// OneShot runs a single compaction request with the given system prompt and
	// serialized conversation payload.
	OneShot(ctx context.Context, systemPrompt, input string) (string, error)
	// CurrentTokens returns the provider's best current token estimate.
	CurrentTokens() int
	// ContextWindow returns the model context window in tokens.
	ContextWindow() int
	// MaxOutputTokens returns the maximum output tokens for the current model.
	MaxOutputTokens() int
}
