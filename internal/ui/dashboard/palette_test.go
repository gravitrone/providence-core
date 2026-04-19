package dashboard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestColorForAgentType_StableAcrossCalls(t *testing.T) {
	names := []string{"researcher", "reviewer", "implementer", "planner", "auditor"}
	for _, n := range names {
		first := HexForAgentType(n)
		for i := 0; i < 32; i++ {
			assert.Equal(t, first, HexForAgentType(n),
				"color for %q must be stable across calls", n)
		}
		// Sanity: the color.Color wrapper should also be stable.
		require.Equal(t, ColorForAgentType(n), ColorForAgentType(n),
			"color.Color wrapper must be stable for %q", n)
	}
}

func TestColorForAgentType_DistinctTypesMapToDifferentColors(t *testing.T) {
	// Pick three distinct names. The 8-color palette cannot guarantee all
	// three are unique under any hash, but a well-spread hash should yield
	// at least two distinct colors for these inputs.
	a := HexForAgentType("researcher")
	b := HexForAgentType("reviewer")
	c := HexForAgentType("implementer")

	distinct := map[string]struct{}{
		a: {},
		b: {},
		c: {},
	}
	assert.GreaterOrEqual(t, len(distinct), 2,
		"at least two of three distinct type names should map to different palette slots (got a=%s b=%s c=%s)", a, b, c)
}

func TestColorForAgentType_EmptyFallback(t *testing.T) {
	got := HexForAgentType("")
	require.Equal(t, agentTypeFallbackHex, got,
		"empty agent-type-name should return the neutral fallback color")
}

func TestColorForAgentType_AllEntriesAreValidHex(t *testing.T) {
	// Guards against a typo in the palette table that would produce an
	// invalid lipgloss color at render time.
	for i, hex := range AgentTypePalette {
		require.Len(t, hex, 7, "palette entry %d must be a 7-char hex string, got %q", i, hex)
		assert.Equal(t, byte('#'), hex[0], "palette entry %d must start with '#'", i)
	}
}

func TestColorForAgentType_SpreadAcrossPalette(t *testing.T) {
	// Sanity: probe a reasonable alphabet and confirm the hash can hit
	// multiple slots. Catches a stuck hash or a palette-length mismatch.
	seen := make(map[string]struct{}, len(AgentTypePalette))
	probes := []string{
		"researcher", "reviewer", "implementer", "planner", "auditor",
		"debugger", "tester", "architect", "librarian", "critic",
		"scout", "janitor", "dispatcher", "scribe", "gardener", "sentinel",
	}
	for _, p := range probes {
		seen[HexForAgentType(p)] = struct{}{}
	}
	assert.GreaterOrEqual(t, len(seen), 4,
		"hash should spread across at least 4 palette slots over a reasonable probe set (got %d)", len(seen))
}

func TestDashboardAgentPanelTypeTint(t *testing.T) {
	// Agents with a type set should still render their names. We do not
	// scrape ANSI here (test-environment sensitive); instead we confirm
	// the palette lookup is stable for the names used, which is the
	// contract the renderer relies on.
	d := newTestDashboard(60, 30)
	d.SetAgents([]AgentInfo{
		{Name: "alpha", Type: "researcher", Model: "opus", Status: "running", Elapsed: "1s"},
		{Name: "beta", Type: "reviewer", Model: "sonnet", Status: "running", Elapsed: "2s"},
	})

	view := d.View()
	assert.Contains(t, view, "alpha", "view should contain agent alpha")
	assert.Contains(t, view, "beta", "view should contain agent beta")

	assert.Equal(t, HexForAgentType("researcher"), HexForAgentType("researcher"),
		"same type-name must yield the same palette color")
}
