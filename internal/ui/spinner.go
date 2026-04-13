package ui

import (
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// spinnerFrames cycles through pulse block patterns at 120ms per frame.
var spinnerFrames = []rune{'\u2588', '\u2593', '\u2592', '\u2591', '\u2592', '\u2593'}

// flameColors is the gradient for spinner color cycling.
// Recomputed on theme switch.
var flameColors []struct{ r, g, b uint8 }

// recomputeSpinnerColors rebuilds flameColors from the active theme.
func recomputeSpinnerColors() {
	darkR, darkG, darkB := hexToRGB(darkenHex(ActiveTheme.Secondary, 0.55))
	midR, midG, midB := hexToRGB(ActiveTheme.Secondary)
	secR, secG, secB := hexToRGB(blendHex(ActiveTheme.Secondary, ActiveTheme.Primary, 0.5))
	brightR, brightG, brightB := hexToRGB(ActiveTheme.Primary)

	flameColors = []struct{ r, g, b uint8 }{
		{darkR, darkG, darkB},
		{secR, secG, secB},
		{midR, midG, midB},
		{brightR, brightG, brightB},
		{midR, midG, midB},
		{secR, secG, secB},
	}
}

// flameColor returns the current flame color based on a frame counter.
func flameColor(frame int) string {
	t := (math.Sin(float64(frame)*0.4) + 1.0) / 2.0

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

// flameTickMsg is sent every 80ms to advance the flame animation.
type flameTickMsg struct{}

func flameTick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return flameTickMsg{}
	})
}

// flameBlockFrames simulates fire flickering with varying width blocks.
var flameBlockFrames = []rune{'\u258C', '\u258D', '\u258E', '\u258F', '\u258E', '\u258D', '\u258C', '\u2588', '\u258A', '\u258B', '\u258C'}

func flameBlock(frame int) (string, string) {
	ch := string(flameBlockFrames[frame%len(flameBlockFrames)])
	color := flameColor(frame)
	return ch, color
}

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

// compactVerbs are the Providence-themed phrases shown while the context
// compaction pipeline is running. Each turns the dry "compacting context"
// into something that matches the Profaned Core aesthetic.
var compactVerbs = []string{
	"Profaning context",
	"Calamitizing memories",
	"Compacting flames",
	"Searing tokens",
	"Rendering the ember",
	"Distilling divine residue",
	"Condensing the brimstone",
	"Crystallizing holy ash",
}

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

type spinnerTickMsg struct{}

func spinnerTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

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

func randomCompactVerb(current string) string {
	if len(compactVerbs) == 1 {
		return compactVerbs[0]
	}
	for {
		v := compactVerbs[rand.IntN(len(compactVerbs))]
		if v != current {
			return v
		}
	}
}

func (at AgentTab) renderSpinner() string {
	if !at.streaming {
		return ""
	}

	frame := string(spinnerFrames[at.spinnerFrame%len(spinnerFrames)])
	elapsed := int(time.Since(at.spinnerStart).Seconds())
	timerStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	// Rate limit countdown takes priority over all other spinner states.
	if at.rateLimitActive {
		remaining := int(time.Until(at.rateLimitExpiry).Seconds())
		if remaining < 0 {
			remaining = 0
		}
		rlSpinnerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(flameColor(at.spinnerFrame))).Bold(true)
		rlVerbStyle := lipgloss.NewStyle().Foreground(ColorWarning).Bold(true)
		rlCountStyle := lipgloss.NewStyle().Foreground(ColorWarning)
		return "  " + rlSpinnerStyle.Render(frame) + " " +
			rlVerbStyle.Render(fmt.Sprintf("Rate limited. Retrying in %ds...", remaining)) + " " +
			rlCountStyle.Render(fmt.Sprintf("(attempt %d/%d)", at.rateLimitAttempt, at.rateLimitMax))
	}

	if at.visualizing {
		vizSpinnerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(flameColor(at.spinnerFrame))).Bold(true)
		vizVerbStyle := lipgloss.NewStyle().Foreground(ColorAccent).Italic(true)
		return "  " + vizSpinnerStyle.Render(frame) + " " +
			vizVerbStyle.Render(at.vizVerb+"...") + " " +
			timerStyle.Render(fmt.Sprintf("(%ds)", elapsed))
	}

	// Active tool progress: show tool name + count + elapsed.
	if at.activeToolName != "" && at.activeToolCount > 0 {
		toolElapsed := int(time.Since(at.activeToolStart).Seconds())
		toolSpinnerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(flameColor(at.spinnerFrame))).Bold(true)
		toolVerbStyle := lipgloss.NewStyle().Foreground(ColorAccent).Italic(true)
		var progressText string
		if at.activeToolCount == 1 {
			progressText = fmt.Sprintf("%s...", at.activeToolName)
		} else {
			progressText = batchVerb(at.activeToolName, at.activeToolCount, false) + "..."
		}
		return "  " + toolSpinnerStyle.Render(frame) + " " +
			toolVerbStyle.Render(progressText) + " " +
			timerStyle.Render(fmt.Sprintf("(%ds)", toolElapsed))
	}

	spinnerStyle := lipgloss.NewStyle().Foreground(ColorWarning)
	verbStyle := lipgloss.NewStyle().Foreground(ColorText).Italic(true)

	return "  " + spinnerStyle.Render(frame) + " " +
		verbStyle.Render(at.spinnerVerb+"...") + " " +
		timerStyle.Render(fmt.Sprintf("(%ds)", elapsed))
}
