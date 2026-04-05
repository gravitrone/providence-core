package ui

import (
	"fmt"
	"image/color"
	"math"
	"math/rand/v2"
	"strings"

	"charm.land/glamour/v2/ansi"
	glamourStyles "charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
)

// --- Flame Gradient Utilities ---

// FlameGradientStops is the 7-stop Providence flame palette for border animations.
var flameGradientStops = []color.Color{
	lipgloss.Color("#4a2010"), // deep ember
	lipgloss.Color("#D77757"), // flame orange
	lipgloss.Color("#FFA600"), // profaned amber
	lipgloss.Color("#FFD700"), // holy gold
	lipgloss.Color("#FFA600"), // profaned amber
	lipgloss.Color("#D77757"), // flame orange
	lipgloss.Color("#4a2010"), // deep ember
}

// FlameShimmerRamp is a precomputed wide ramp for tool name shimmer effects.
// Generated once at init from the flame palette, 60 steps wide.
var flameShimmerRamp []color.Color

func init() {
	flameShimmerRamp = lipgloss.Blend1D(60,
		lipgloss.Color("#6b3a1a"), // deep ember
		lipgloss.Color("#FFA600"), // profaned amber
		lipgloss.Color("#FFD700"), // holy gold (bright peak)
		lipgloss.Color("#FFA600"), // profaned amber
		lipgloss.Color("#6b3a1a"), // deep ember
	)
}

// RenderToolShimmer applies a per-character gradient shimmer to text.
// The bright peak travels across the letters each frame.
func renderToolShimmer(text string, frame int) string {
	runes := []rune(text)
	n := len(runes)
	if n == 0 {
		return ""
	}

	rampSize := len(flameShimmerRamp)
	offset := (frame * 2) % rampSize

	var b strings.Builder
	for i, r := range runes {
		colorIdx := (offset + i) % rampSize
		b.WriteString(lipgloss.NewStyle().
			Foreground(flameShimmerRamp[colorIdx]).
			Bold(true).
			Render(string(r)))
	}
	return b.String()
}

// SpinnerScrambleChars is the charset for the scrambling spinner character.
var spinnerScrambleChars = []rune("0123456789abcdefABCDEF~*+=")

// RenderScrambleChar returns a single scrambling character colored with flame gradient.
func renderScrambleChar(frame int) string {
	ch := spinnerScrambleChars[rand.IntN(len(spinnerScrambleChars))]
	fc := flameColor(frame)
	return lipgloss.NewStyle().Foreground(lipgloss.Color(fc)).Bold(true).Render(string(ch))
}

// GradientDivider renders a horizontal divider that fades from near-invisible
// edges to a warm center.
func gradientDivider(width int) string {
	if width <= 0 {
		return ""
	}
	ramp := lipgloss.Blend1D(width,
		lipgloss.Color("#1a1210"), // edge: near invisible
		lipgloss.Color("#3a2518"), // mid: dark ember
		lipgloss.Color("#6b3a1a"), // center: warm
		lipgloss.Color("#3a2518"), // mid
		lipgloss.Color("#1a1210"), // edge
	)
	var b strings.Builder
	for i := 0; i < width; i++ {
		b.WriteString(lipgloss.NewStyle().Foreground(ramp[i]).Render("\u2500"))
	}
	return b.String()
}

// CompletionCoolRamp is a precomputed 100-step ramp from frozen ember to bright gold,
// used for the completion spring animation. Index 0 = ember, index 99 = gold.
var completionCoolRamp []color.Color

func init() {
	completionCoolRamp = lipgloss.Blend1D(100,
		lipgloss.Color("#A0704A"), // frozen ember (brightness = 0.0)
		lipgloss.Color("#D77757"), // flame orange
		lipgloss.Color("#FFA600"), // profaned amber
		lipgloss.Color("#FFD700"), // bright gold (brightness = 1.0)
	)
}

// CompletionColor returns an interpolated color based on brightness (0.0 = ember, 1.0 = gold).
// Driven by harmonica spring physics.
func completionColor(brightness float64) color.Color {
	idx := int(brightness * 99)
	if idx < 0 {
		idx = 0
	}
	if idx > 99 {
		idx = 99
	}
	return completionCoolRamp[idx]
}

// BannerAnimGradient is a wider gradient for the banner animation (16 stops for smooth cycling).
var bannerAnimGradient []color.Color

func init() {
	bannerAnimGradient = lipgloss.Blend1D(16,
		lipgloss.Color("#4c2210"), // deep ember
		lipgloss.Color("#743a1e"),
		lipgloss.Color("#9c5232"),
		lipgloss.Color("#D77757"), // profaned flame (peak)
		lipgloss.Color("#c46a4a"),
		lipgloss.Color("#9c5232"),
		lipgloss.Color("#743a1e"),
		lipgloss.Color("#4c2210"), // deep ember
	)
}

// RenderBannerAnimated returns the styled ASCII banner with a shifting flame gradient.
// Always animates: faster scroll during streaming (every frame), slower when idle (every 3rd frame).
func RenderBannerAnimated(frame int, streaming bool) string {
	rendered := "\n"

	// Trim trailing spaces from all lines so centering is based on visible content.
	trimmed := make([]string, len(bannerLines))
	maxW := 0
	for i, line := range bannerLines {
		trimmed[i] = strings.TrimRight(line, " ")
		if w := lipgloss.Width(trimmed[i]); w > maxW {
			maxW = w
		}
	}

	// Always slow subtle animation regardless of streaming state.
	shift := frame / 8

	rampSize := len(bannerAnimGradient)
	for i, line := range trimmed {
		colorIdx := (i + shift) % rampSize
		style := lipgloss.NewStyle().Foreground(bannerAnimGradient[colorIdx])
		rendered += style.Render(line) + "\n"
	}

	// Blank line + subtitle (ember breathing) + underline.
	rendered += "\n"
	subtitleText := "The Profaned Core"
	subtitleWidth := lipgloss.Width(subtitleText)

	subtitleStyle := lipgloss.NewStyle().
		Foreground(emberBreathe(frame)).
		Width(maxW).
		Align(lipgloss.Center)
	rendered += subtitleStyle.Render(subtitleText) + "\n"

	underlineStyle := lipgloss.NewStyle().
		Foreground(ColorBorder).
		Width(maxW).
		Align(lipgloss.Center)
	rendered += underlineStyle.Render(strings.Repeat("\u2500", subtitleWidth)) + "\n"

	return rendered
}

// EmberBreathe returns a color that slowly oscillates between two muted ember tones.
// Full cycle is ~3.5 seconds (44 frames at 80ms). Very subtle, like embers glowing in the dark.
func emberBreathe(frame int) color.Color {
	// Slow sine wave: 0.14 radians per frame = ~44 frames per full cycle = ~3.5s at 80ms tick.
	t := (math.Sin(float64(frame)*0.14) + 1.0) / 2.0 // 0.0 to 1.0

	// Interpolate between #B85A38 (dim ember) and #D77757 (warm flame orange).
	r := uint8(0xB8 + t*float64(0xD7-0xB8))
	g := uint8(0x5A + t*float64(0x77-0x5A))
	b := uint8(0x38 + t*float64(0x57-0x38))

	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, b))
}

// EmberBreatheD is a dimmer variant of emberBreathe with half the amplitude.
// Stays closer to muted ash - barely a visible shift, like cooling embers at the edge of dark.
func emberBreatheD(frame int) color.Color {
	// Same sine wave, but amplitude is halved - only reaches halfway to full ember glow.
	t := (math.Sin(float64(frame)*0.14) + 1.0) / 4.0 // 0.0 to 0.5 range

	// Interpolate between #6b5040 (muted ash) and #B85A38 (dim ember) - lower ceiling.
	r := uint8(0x6b + t*float64(0xB8-0x6b))
	g := uint8(0x50 + t*float64(0x5A-0x50))
	b := uint8(0x40 + t*float64(0x38-0x40))

	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, b))
}

// ProvidenceGlamourStyle returns a glamour StyleConfig themed for Providence.
// Headers in flame orange, links in amber, code blocks warm-tinted.
func providenceGlamourStyle() ansi.StyleConfig {
	style := glamourStyles.DarkStyleConfig

	zeroMargin := uint(0)
	style.Document.Margin = &zeroMargin

	// Headers: flame orange.
	flameOrange := strPtr("#D77757")
	amber := strPtr("#FFA600")
	warmText := strPtr("#e0d0c0")
	mutedEmber := strPtr("#6b5040")
	trueBool := boolPtr(true)

	style.H1.Color = amber
	style.H1.Bold = trueBool
	style.H2.Color = flameOrange
	style.H2.Bold = trueBool
	style.H3.Color = flameOrange
	style.H3.Bold = trueBool
	style.H4.Color = flameOrange
	style.H5.Color = flameOrange
	style.H6.Color = mutedEmber

	// Links: amber.
	style.Link.Color = amber
	style.LinkText.Color = amber

	// Bold/emphasis: warm.
	style.Emph.Color = warmText
	style.Strong.Color = warmText
	style.Strong.Bold = trueBool

	// Code: warm tinted.
	style.Code.Color = amber
	style.CodeBlock.Theme = "native"

	return style
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
