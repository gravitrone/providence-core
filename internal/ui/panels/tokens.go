package panels

import (
	"fmt"
	"strings"
)

// RenderTokens shows context window fill with a gradient bar.
func RenderTokens(current, max int, width int) string {
	if max == 0 {
		return "  0% context used"
	}
	pct := current * 100 / max
	barW := width - 4
	if barW < 1 {
		barW = 1
	}
	filled := barW * pct / 100

	// Build gradient bar: filled blocks vs empty blocks.
	var b strings.Builder
	for i := range barW {
		if i < filled {
			b.WriteRune('\u2588') // filled block
		} else {
			b.WriteRune('\u2591') // empty block
		}
	}

	return fmt.Sprintf("  [%s] %d%%\n  %dK / %dK tokens", b.String(), pct, current/1000, max/1000)
}
