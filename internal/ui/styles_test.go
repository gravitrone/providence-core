package ui

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
)

// TestThemeColorsAreDefined verifies each exported color is a non-nil color.Color.
func TestThemeColorsAreDefined(t *testing.T) {
	tests := []struct {
		name  string
		color color.Color
	}{
		{"ColorPrimary", ColorPrimary},
		{"ColorSecondary", ColorSecondary},
		{"ColorAccent", ColorAccent},
		{"ColorBackground", ColorBackground},
		{"ColorText", ColorText},
		{"ColorMuted", ColorMuted},
		{"ColorSuccess", ColorSuccess},
		{"ColorError", ColorError},
		{"ColorWarning", ColorWarning},
		{"ColorBorder", ColorBorder},
		{"ColorGlow", ColorGlow},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.NotNil(t, tc.color)
			// All colors should have valid RGBA values.
			r, g, b, a := tc.color.RGBA()
			_ = r
			_ = g
			_ = b
			assert.Equal(t, uint32(0xffff), a, "color %s alpha should be fully opaque", tc.name)
		})
	}
}

// TestStylesRenderNonEmpty verifies each exported style produces non-empty output.
func TestStylesRenderNonEmpty(t *testing.T) {
	const sample = "test"
	tests := []struct {
		name  string
		style lipgloss.Style
	}{
		{"BannerStyle", BannerStyle},
		{"BannerAccentStyle", BannerAccentStyle},
		{"TabActiveStyle", TabActiveStyle},
		{"TabInactiveStyle", TabInactiveStyle},
		{"StatusBarStyle", StatusBarStyle},
		{"SelectedStyle", SelectedStyle},
		{"NormalStyle", NormalStyle},
		{"MutedStyle", MutedStyle},
		{"SuccessStyle", SuccessStyle},
		{"ErrorStyle", ErrorStyle},
		{"WarningStyle", WarningStyle},
		{"AccentStyle", AccentStyle},
		{"GlowStyle", GlowStyle},
		{"HeaderStyle", HeaderStyle},
		{"BorderStyle", BorderStyle},
		{"TypeBadgeStyle", TypeBadgeStyle},
		{"DividerStyle", DividerStyle},
		{"MetaKeyStyle", MetaKeyStyle},
		{"MetaValueStyle", MetaValueStyle},
		{"MetaPunctStyle", MetaPunctStyle},
		{"ScoreHighStyle", ScoreHighStyle},
		{"ScoreMedStyle", ScoreMedStyle},
		{"ScoreLowStyle", ScoreLowStyle},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rendered := tc.style.Render(sample)
			assert.NotEmpty(t, rendered)
			assert.Contains(t, rendered, sample, "rendered output should contain the input text")
		})
	}
}

func TestDividerEmptyOnZeroWidth(t *testing.T) {
	out := Divider(0)
	assert.Empty(t, out)
}

func TestDividerEmptyOnNegativeWidth(t *testing.T) {
	out := Divider(-5)
	assert.Empty(t, out)
}

func TestDividerProducesOutput(t *testing.T) {
	out := Divider(10)
	assert.NotEmpty(t, out)
}

func TestDividerWidthMatchesRunes(t *testing.T) {
	const width = 20
	out := Divider(width)
	assert.Equal(t, width, lipgloss.Width(out))
}

func TestBannerStyleIsBold(t *testing.T) {
	assert.True(t, BannerStyle.GetBold())
}

func TestErrorStyleIsBold(t *testing.T) {
	assert.True(t, ErrorStyle.GetBold())
}

func TestTabActiveStyleRendersText(t *testing.T) {
	rendered := TabActiveStyle.Render("Jobs")
	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "Jobs")
}

func TestTabActiveAndInactiveDiffer(t *testing.T) {
	active := TabActiveStyle.Render("Tab")
	inactive := TabInactiveStyle.Render("Tab")
	// Different styles should produce different ANSI output.
	assert.NotEqual(t, active, inactive)
}
