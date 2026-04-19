package dashboard

import (
	"hash/fnv"
	"image/color"

	"charm.land/lipgloss/v2"
)

// --- Agent Type Palette ---

// AgentTypePalette is the stable 8-entry palette used to tint subagent
// rows by agent-type-name. Entries are hex strings so the palette is
// cheap to compare, log, and serialize; they are converted to
// lipgloss colors at lookup time via ColorForAgentType.
//
// Colors harmonize with the Providence fire theme: warm oranges, reds,
// and golds form the baseline, with a couple of cooler contrast tones
// (teal, violet) so distinct types stay readable without introducing
// jarring saturated hues.
var AgentTypePalette = [8]string{
	"#FFA600", // 0: amber (primary flame)
	"#D77757", // 1: ember red-orange
	"#FFD166", // 2: pale gold
	"#E76F51", // 3: coral red
	"#F4A261", // 4: sandstone
	"#C98A5B", // 5: bronze
	"#7FB8A4", // 6: cool teal (contrast)
	"#B08CC8", // 7: muted violet (contrast)
}

// agentTypeFallbackHex is used when the agent-type-name is empty.
// It matches the dashboard's text tone so unlabeled agents stay neutral.
const agentTypeFallbackHex = "#E8DACE"

// ColorForAgentType returns a stable palette color for the given
// agent-type-name. Hashing is deterministic across runs, so the same
// type-name always maps to the same palette slot. Empty names fall
// back to a neutral text color so unlabeled agents do not get tinted.
func ColorForAgentType(typeName string) color.Color {
	return lipgloss.Color(HexForAgentType(typeName))
}

// HexForAgentType returns the stable palette hex string for a given
// agent-type-name. Useful for tests and for callers that want to
// compare palette assignments without going through a color.Color
// value.
func HexForAgentType(typeName string) string {
	if typeName == "" {
		return agentTypeFallbackHex
	}
	h := fnv.New32a()
	// fnv writer never returns an error.
	_, _ = h.Write([]byte(typeName))
	idx := h.Sum32() % uint32(len(AgentTypePalette))
	return AgentTypePalette[idx]
}
