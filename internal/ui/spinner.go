package ui

import (
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// SpinnerFrames cycles through pulse block patterns at 120ms per frame.
var spinnerFrames = []rune{'█', '▓', '▒', '░', '▒', '▓'}

// FlameColors is the Providence flame gradient: subtle warm range, not epileptic.
var flameColors = []struct{ r, g, b uint8 }{
	{180, 90, 40},  // warm ember
	{200, 105, 55}, // mid flame
	{215, 119, 87}, // #D77757 flame orange
	{230, 140, 70}, // bright flame
	{215, 119, 87}, // #D77757 flame orange
	{200, 105, 55}, // mid flame
}

// FlameColor returns the current flame color based on a frame counter.
// Uses sine wave interpolation for smooth breathing effect.
func flameColor(frame int) string {
	// Sine wave oscillation: 0→1→0 over ~2 seconds (16 frames at 120ms).
	t := (math.Sin(float64(frame)*0.4) + 1.0) / 2.0 // 0.0 to 1.0

	// Interpolate between dark ember and gold.
	idx := t * float64(len(flameColors)-1)
	lo := int(idx)
	hi := lo + 1
	if hi >= len(flameColors) {
		hi = len(flameColors) - 1
	}
	frac := idx - float64(lo)

	r := uint8(float64(flameColors[lo].r) + frac*float64(int(flameColors[hi].r)-int(flameColors[lo].r)))
	g := uint8(float64(flameColors[lo].g) + frac*float64(int(flameColors[hi].g)-int(flameColors[lo].g)))
	b := uint8(float64(flameColors[lo].b) + frac*float64(int(flameColors[hi].b)-int(flameColors[lo].b)))

	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// FlameTickMsg is sent every 80ms to advance the flame animation.
type flameTickMsg struct{}

// FlameTick returns a Cmd that fires a flameTickMsg after 80ms.
func flameTick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return flameTickMsg{}
	})
}

// FlameBlockFrames simulates fire flickering with varying width blocks.
var flameBlockFrames = []rune{'▌', '▍', '▎', '▏', '▎', '▍', '▌', '█', '▊', '▋', '▌'}

// FlameBlock returns the current flame block character and color for the given frame.
func flameBlock(frame int) (string, string) {
	ch := string(flameBlockFrames[frame%len(flameBlockFrames)])
	color := flameColor(frame)
	return ch, color
}

// SpinnerVerbs is the Providence + Calamity themed verb list.
var spinnerVerbs = []string{
	"Profaning",
	"Purifying",
	"Consecrating",
	"Sanctifying",
	"Immolating",
	"Smiting",
	"Judging",
	"Ascending",
	"Divining",
	"Incinerating",
	"Brimstoning",
	"Devouring",
	"Calamitizing",
}

// VizVerbs is the Providence + Calamity themed verb list for visualizations.
var vizVerbs = []string{
	"Conjuring the flames",
	"Forging divine sight",
	"Manifesting brimstone",
	"Channeling the profaned eye",
	"Rendering holy fire",
	"Crystallizing ember visions",
	"Weaving flame tapestry",
	"Invoking sacred geometry",
	"Summoning the burning canvas",
	"Igniting astral projection",
	"Etching in divine light",
	"Scorching reality into form",
}

// SpinnerTickMsg is sent every 120ms to advance the spinner animation.
type spinnerTickMsg struct{}

// SpinnerTick returns a Cmd that fires a spinnerTickMsg after 120ms.
func spinnerTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

// RandomVerb picks a random verb from spinnerVerbs, avoiding the current one if possible.
func randomVerb(current string) string {
	if len(spinnerVerbs) == 1 {
		return spinnerVerbs[0]
	}
	for {
		v := spinnerVerbs[rand.IntN(len(spinnerVerbs))]
		if v != current {
			return v
		}
	}
}

// RandomVizVerb picks a random viz verb, avoiding the current one.
func randomVizVerb(current string) string {
	if len(vizVerbs) == 1 {
		return vizVerbs[0]
	}
	for {
		v := vizVerbs[rand.IntN(len(vizVerbs))]
		if v != current {
			return v
		}
	}
}

// RenderSpinner returns the spinner line string, or "" if not streaming.
// Uses the original pulse block animation (█▓▒░) for thinking verbs.
// When visualizing, shows viz verbs with a hotter flame color.
func (at AgentTab) renderSpinner() string {
	if !at.streaming {
		return ""
	}

	frame := string(spinnerFrames[at.spinnerFrame%len(spinnerFrames)])
	elapsed := int(time.Since(at.spinnerStart).Seconds())
	timerStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	if at.visualizing {
		// Hotter flame for viz: gold/amber range instead of yellow.
		vizSpinnerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(flameColor(at.spinnerFrame))).Bold(true)
		vizVerbStyle := lipgloss.NewStyle().Foreground(ColorAccent).Italic(true)
		return "  " + vizSpinnerStyle.Render(frame) + " " +
			vizVerbStyle.Render(at.vizVerb+"...") + " " +
			timerStyle.Render(fmt.Sprintf("(%ds)", elapsed))
	}

	spinnerStyle := lipgloss.NewStyle().Foreground(ColorWarning)
	verbStyle := lipgloss.NewStyle().Foreground(ColorText).Italic(true)

	return "  " + spinnerStyle.Render(frame) + " " +
		verbStyle.Render(at.spinnerVerb+"...") + " " +
		timerStyle.Render(fmt.Sprintf("(%ds)", elapsed))
}
