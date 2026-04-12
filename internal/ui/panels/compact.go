package panels

import "fmt"

// CompactInfo represents the compaction state.
type CompactInfo struct {
	LastRun      string
	BeforePct    int
	AfterPct     int
	ThresholdPct int
	Mode         string // cc-tail-replace, dynamic-rolling, both
}

// RenderCompact shows compaction state.
func RenderCompact(state CompactInfo, width int) string {
	if state.LastRun == "" {
		return "  Never compacted"
	}
	return fmt.Sprintf("  Last: %s (%d%% -> %d%%)\n  Trigger: ~%d%% fill\n  Mode: %s",
		state.LastRun, state.BeforePct, state.AfterPct, state.ThresholdPct, state.Mode)
}
