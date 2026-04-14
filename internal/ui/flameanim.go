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

// flameGradientStops is the 7-stop Providence palette for border animations.
// Recomputed on theme switch.
var flameGradientStops []color.Color

// flameShimmerRamp is a precomputed wide ramp for tool name shimmer effects.
// 60 steps wide. Recomputed on theme switch.
var flameShimmerRamp []color.Color

// completionCoolRamp is a precomputed 100-step ramp from frozen to bright,
// used for the completion spring animation. Recomputed on theme switch.
var completionCoolRamp []color.Color

// bannerAnimGradient is a wider gradient for the banner animation (16 stops).
// Recomputed on theme switch.
var bannerAnimGradient []color.Color

// recomputeFlameGradients rebuilds all precomputed gradients from the active theme.
// Called from ApplyTheme.
func recomputeFlameGradients() {
	deepEmber := darkenHex(ActiveTheme.Secondary, 0.35)

	flameGradientStops = []color.Color{
		c(deepEmber),
		c(ActiveTheme.Secondary),
		c(ActiveTheme.Primary),
		c(ActiveTheme.Accent),
		c(ActiveTheme.Primary),
		c(ActiveTheme.Secondary),
		c(deepEmber),
	}

	shimmerDeep := darkenHex(ActiveTheme.Primary, 0.4)
	flameShimmerRamp = lipgloss.Blend1D(60,
		c(shimmerDeep),
		c(ActiveTheme.Primary),
		c(ActiveTheme.Accent),
		c(ActiveTheme.Primary),
		c(shimmerDeep),
	)

	completionCoolRamp = lipgloss.Blend1D(100,
		c(ActiveTheme.Frozen),
		c(ActiveTheme.Secondary),
		c(ActiveTheme.Primary),
		c(ActiveTheme.Accent),
	)

	bannerDeep := darkenHex(ActiveTheme.Secondary, 0.3)
	bannerMid1 := darkenHex(ActiveTheme.Secondary, 0.5)
	bannerMid2 := darkenHex(ActiveTheme.Secondary, 0.65)
	bannerPeak := blendHex(ActiveTheme.Secondary, ActiveTheme.Primary, 0.3)
	bannerAnimGradient = lipgloss.Blend1D(16,
		c(bannerDeep),
		c(bannerMid2),
		c(bannerMid1),
		c(ActiveTheme.Secondary),
		c(bannerPeak),
		c(bannerMid1),
		c(bannerMid2),
		c(bannerDeep),
	)
}

// darkenHex returns a darkened hex color string.
// factor 0.0 = black, 1.0 = original color.
func darkenHex(hex string, factor float64) string {
	r, g, b := hexToRGB(hex)
	dr := uint8(float64(r) * factor)
	dg := uint8(float64(g) * factor)
	db := uint8(float64(b) * factor)
	return fmt.Sprintf("#%02x%02x%02x", dr, dg, db)
}

// lightenHex returns a lightened hex color string.
// factor 0.0 = original, 1.0 = white.
func lightenHex(hex string, factor float64) string {
	r, g, b := hexToRGB(hex)
	lr := uint8(float64(r) + factor*float64(255-int(r)))
	lg := uint8(float64(g) + factor*float64(255-int(g)))
	lb := uint8(float64(b) + factor*float64(255-int(b)))
	return fmt.Sprintf("#%02x%02x%02x", lr, lg, lb)
}

// blendHex blends two hex colors. t=0.0 returns a, t=1.0 returns b.
func blendHex(a, b string, t float64) string {
	ar, ag, ab := hexToRGB(a)
	br, bg, bb := hexToRGB(b)
	r := uint8(float64(ar) + t*float64(int(br)-int(ar)))
	g := uint8(float64(ag) + t*float64(int(bg)-int(ag)))
	bl := uint8(float64(ab) + t*float64(int(bb)-int(ab)))
	return fmt.Sprintf("#%02x%02x%02x", r, g, bl)
}

// hexToRGB parses a hex color string to RGB components.
func hexToRGB(hex string) (uint8, uint8, uint8) {
	if len(hex) > 0 && hex[0] == '#' {
		hex = hex[1:]
	}
	if len(hex) != 6 {
		return 0, 0, 0
	}
	var r, g, b uint8
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}

// renderToolShimmer applies a per-character gradient shimmer to text.
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

// spinnerScrambleChars is the charset for the scrambling spinner character.
var spinnerScrambleChars = []rune("0123456789abcdefABCDEF~*+=")

// renderScrambleChar returns a single scrambling character colored with flame gradient.
func renderScrambleChar(frame int) string {
	ch := spinnerScrambleChars[rand.IntN(len(spinnerScrambleChars))]
	fc := flameColor(frame)
	return lipgloss.NewStyle().Foreground(lipgloss.Color(fc)).Bold(true).Render(string(ch))
}

// gradientDivider renders a horizontal divider that fades from near-invisible
// edges to a warm center. Uses theme colors.
func gradientDivider(width int) string {
	if width <= 0 {
		return ""
	}
	edgeColor := darkenHex(ActiveTheme.Border, 0.7)
	centerColor := darkenHex(ActiveTheme.Primary, 0.4)
	ramp := lipgloss.Blend1D(width,
		c(edgeColor),
		c(ActiveTheme.Border),
		c(centerColor),
		c(ActiveTheme.Border),
		c(edgeColor),
	)
	var b strings.Builder
	for i := 0; i < width; i++ {
		b.WriteString(lipgloss.NewStyle().Foreground(ramp[i]).Render("\u2500"))
	}
	return b.String()
}

// completionColor returns an interpolated color based on brightness (0.0 = frozen, 1.0 = bright).
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

// RenderBannerAnimated returns the styled ASCII banner with a shifting gradient.
func RenderBannerAnimated(frame int, streaming bool) string {
	rendered := "\n"

	trimmed := make([]string, len(bannerLines))
	maxW := 0
	for i, line := range bannerLines {
		trimmed[i] = strings.TrimRight(line, " ")
		if w := lipgloss.Width(trimmed[i]); w > maxW {
			maxW = w
		}
	}

	shift := frame / 8

	rampSize := len(bannerAnimGradient)
	for i, line := range trimmed {
		colorIdx := (i + shift) % rampSize
		style := lipgloss.NewStyle().Foreground(bannerAnimGradient[colorIdx])
		rendered += style.Render(line) + "\n"
	}

	rendered += "\n"
	subtitleText := "The Profaned Core"

	subtitleStyle := lipgloss.NewStyle().
		Foreground(emberBreathe(frame)).
		Width(maxW).
		Align(lipgloss.Center)
	rendered += subtitleStyle.Render(subtitleText) + "\n"

	return rendered
}

// emberBreathe returns a color that slowly oscillates between two muted tones.
// Uses theme colors for theme-aware breathing.
func emberBreathe(frame int) color.Color {
	t := (math.Sin(float64(frame)*0.14) + 1.0) / 2.0

	dimHex := darkenHex(ActiveTheme.Secondary, 0.68)
	dimR, dimG, dimB := hexToRGB(dimHex)
	secR, secG, secB := hexToRGB(ActiveTheme.Secondary)

	r := uint8(float64(dimR) + t*float64(int(secR)-int(dimR)))
	g := uint8(float64(dimG) + t*float64(int(secG)-int(dimG)))
	b := uint8(float64(dimB) + t*float64(int(secB)-int(dimB)))

	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, b))
}

// emberBreatheD is a dimmer variant of emberBreathe with half the amplitude.
func emberBreatheD(frame int) color.Color {
	t := (math.Sin(float64(frame)*0.14) + 1.0) / 4.0

	mutR, mutG, mutB := hexToRGB(ActiveTheme.Muted)
	dimHex := darkenHex(ActiveTheme.Secondary, 0.68)
	dimR, dimG, dimB := hexToRGB(dimHex)

	r := uint8(float64(mutR) + t*float64(int(dimR)-int(mutR)))
	g := uint8(float64(mutG) + t*float64(int(dimG)-int(mutG)))
	b := uint8(float64(mutB) + t*float64(int(dimB)-int(mutB)))

	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, b))
}

// animatedDivider renders a full-width gradient ─ line that pulses from edges to center.
// Adapted from wifey-bench: edges dim, center pulses with flame theme colors.
func animatedDivider(width, frame int) string {
	if width < 3 {
		return ""
	}
	// Edge color: dimmed secondary.
	edgeHex := darkenHex(ActiveTheme.Secondary, 0.3)
	// Center color: pulsing between secondary and bright.
	t := (math.Sin(float64(frame)*0.12) + 1.0) / 2.0
	secR, secG, secB := hexToRGB(ActiveTheme.Secondary)
	brightR, brightG, brightB := hexToRGB(darkenHex(ActiveTheme.Secondary, 1.3))
	centerR := uint8(float64(secR) + t*float64(int(brightR)-int(secR)))
	centerG := uint8(float64(secG) + t*float64(int(brightG)-int(secG)))
	centerB := uint8(float64(secB) + t*float64(int(brightB)-int(secB)))
	centerHex := fmt.Sprintf("#%02x%02x%02x", centerR, centerG, centerB)

	edgeR, edgeG, edgeB := hexToRGB(edgeHex)
	midR, midG, midB := hexToRGB(ActiveTheme.Secondary)

	var b strings.Builder
	half := width / 2
	for i := 0; i < width; i++ {
		var r, g, bb uint8
		var progress float64
		if i <= half {
			// Left edge → center: edge → mid → center
			progress = float64(i) / float64(half)
			if progress < 0.5 {
				p := progress * 2
				r = uint8(float64(edgeR) + p*float64(int(midR)-int(edgeR)))
				g = uint8(float64(edgeG) + p*float64(int(midG)-int(edgeG)))
				bb = uint8(float64(edgeB) + p*float64(int(midB)-int(edgeB)))
			} else {
				p := (progress - 0.5) * 2
				cR, cG, cB := hexToRGB(centerHex)
				r = uint8(float64(midR) + p*float64(int(cR)-int(midR)))
				g = uint8(float64(midG) + p*float64(int(cG)-int(midG)))
				bb = uint8(float64(midB) + p*float64(int(cB)-int(midB)))
			}
		} else {
			// Center → right edge: center → mid → edge
			progress = float64(i-half) / float64(width-half)
			if progress < 0.5 {
				p := progress * 2
				cR, cG, cB := hexToRGB(centerHex)
				r = uint8(float64(cR) + p*float64(int(midR)-int(cR)))
				g = uint8(float64(cG) + p*float64(int(midG)-int(cG)))
				bb = uint8(float64(cB) + p*float64(int(midB)-int(cB)))
			} else {
				p := (progress - 0.5) * 2
				r = uint8(float64(midR) + p*float64(int(edgeR)-int(midR)))
				g = uint8(float64(midG) + p*float64(int(edgeG)-int(midG)))
				bb = uint8(float64(midB) + p*float64(int(edgeB)-int(midB)))
			}
		}
		hex := fmt.Sprintf("#%02x%02x%02x", r, g, bb)
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("─"))
	}
	return b.String()
}

// pulseColor returns a color that breathes between a dimmed and full version of baseHex.
// Use for pulsating borders on permission dialogs and completed tool boxes.
func pulseColor(frame int, baseHex string) color.Color {
	t := (math.Sin(float64(frame)*0.14) + 1.0) / 2.0
	dimR, dimG, dimB := hexToRGB(darkenHex(baseHex, 0.4))
	fullR, fullG, fullB := hexToRGB(baseHex)
	r := uint8(float64(dimR) + t*float64(int(fullR)-int(dimR)))
	g := uint8(float64(dimG) + t*float64(int(fullG)-int(dimG)))
	b := uint8(float64(dimB) + t*float64(int(fullB)-int(dimB)))
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, b))
}

// providenceGlamourStyle returns a glamour StyleConfig themed for Providence.
func providenceGlamourStyle() ansi.StyleConfig {
	style := glamourStyles.DarkStyleConfig

	zeroMargin := uint(0)
	style.Document.Margin = &zeroMargin

	secondary := strPtr(ActiveTheme.Secondary)
	primary := strPtr(ActiveTheme.Primary)
	text := strPtr(ActiveTheme.Text)
	muted := strPtr(ActiveTheme.Muted)
	trueBool := boolPtr(true)

	style.H1.Color = primary
	style.H1.Bold = trueBool
	style.H2.Color = secondary
	style.H2.Bold = trueBool
	style.H3.Color = secondary
	style.H3.Bold = trueBool
	style.H4.Color = secondary
	style.H5.Color = secondary
	style.H6.Color = muted

	style.Link.Color = primary
	style.LinkText.Color = primary

	style.Emph.Color = text
	style.Strong.Color = text
	style.Strong.Bold = trueBool

	style.Code.Color = primary
	style.CodeBlock.Theme = "native"

	return style
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
