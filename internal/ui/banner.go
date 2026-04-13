package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

var bannerLines = []string{
	" ███████████  ███████████      ███████    █████   █████ █████ ██████████   ██████████ ██████   █████   █████████  ██████████",
	"░░███░░░░░███░░███░░░░░███   ███░░░░░███ ░░███   ░░███ ░░███ ░░███░░░░███ ░░███░░░░░█░░██████ ░░███   ███░░░░░███░░███░░░░░█",
	" ░███    ░███ ░███    ░███  ███     ░░███ ░███    ░███  ░███  ░███   ░░███ ░███  █ ░  ░███░███ ░███  ███     ░░░  ░███  █ ░ ",
	" ░██████████  ░██████████  ░███      ░███ ░███    ░███  ░███  ░███    ░███ ░██████    ░███░░███░███ ░███          ░██████   ",
	" ░███░░░░░░   ░███░░░░░███ ░███      ░███ ░░███   ███   ░███  ░███    ░███ ░███░░█    ░███ ░░██████ ░███          ░███░░█   ",
	" ░███         ░███    ░███ ░░███     ███   ░░░█████░    ░███  ░███    ███  ░███ ░   █ ░███  ░░█████ ░░███     ███ ░███ ░   █",
	" █████        █████   █████ ░░░███████░      ░░███      █████ ██████████   ██████████ █████  ░░█████ ░░█████████  ██████████",
	"░░░░░        ░░░░░   ░░░░░    ░░░░░░░         ░░░      ░░░░░ ░░░░░░░░░░   ░░░░░░░░░░ ░░░░░    ░░░░░   ░░░░░░░░░  ░░░░░░░░░░ ",
}

// bannerSubtitle is the text shown below the ASCII art. Updated on engine switch.
var bannerSubtitle = "The Profaned Core"

// SetBannerSubtitle changes the banner subtitle (e.g. on engine switch).
func SetBannerSubtitle(s string) {
	bannerSubtitle = s
}

// bannerGradient is a top-to-bottom gradient for the static banner.
// Recomputed on theme switch.
var bannerGradient []string

// recomputeBannerGradient rebuilds the static banner gradient from the active theme.
func recomputeBannerGradient() {
	steps := 8
	secR, secG, secB := hexToRGB(ActiveTheme.Secondary)
	darkHex := darkenHex(ActiveTheme.Secondary, 0.3)
	darkR, darkG, darkB := hexToRGB(darkHex)

	bannerGradient = make([]string, steps)
	for i := range steps {
		t := float64(i) / float64(steps-1)
		r := uint8(float64(secR) + t*float64(int(darkR)-int(secR)))
		g := uint8(float64(secG) + t*float64(int(darkG)-int(secG)))
		b := uint8(float64(secB) + t*float64(int(darkB)-int(secB)))
		bannerGradient[i] = fmt.Sprintf("#%02x%02x%02x", r, g, b)
	}
}

// RenderBanner returns the styled ASCII banner with a gradient and underline.
func RenderBanner() string {
	rendered := "\n"

	trimmed := make([]string, len(bannerLines))
	maxW := 0
	for i, line := range bannerLines {
		trimmed[i] = strings.TrimRight(line, " ")
		if w := lipgloss.Width(trimmed[i]); w > maxW {
			maxW = w
		}
	}

	for i, line := range trimmed {
		clr := bannerGradient[i]
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(clr))
		rendered += style.Render(line) + "\n"
	}

	rendered += "\n"
	subtitleText := bannerSubtitle
	subtitleWidth := lipgloss.Width(subtitleText)

	subtitleStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
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
