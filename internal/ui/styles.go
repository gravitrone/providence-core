package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gravitrone/providence-core/internal/ui/components"
)

// --- Theme System (Providence - The Profaned Goddess) ---

// ThemeColors defines a complete color palette for a Providence theme.
// Colors stored as hex strings for easy gradient math.
type ThemeColors struct {
	Primary    string
	Secondary  string
	Accent     string
	Background string
	Text       string
	Muted      string
	Success    string
	Error      string
	Warning    string
	Border     string
	Card       string
	Glow       string
	Frozen     string
}

// FlameTheme is the day form of Providence: fire, orange, gold.
var FlameTheme = ThemeColors{
	Primary:    "#FFA600", // Profaned amber
	Secondary:  "#D77757", // Flame orange
	Accent:     "#FFD700", // Holy gold
	Background: "#0a0a0a", // Void black
	Text:       "#e0d0c0", // Warm divine light
	Muted:      "#6b5040", // Ember ash
	Success:    "#19FA19", // Profaned emerald
	Error:      "#ff5555", // Brimstone red
	Warning:    "#FFD700", // Holy gold
	Border:     "#3a2518", // Dark ember
	Card:       "#1a1210", // Profaned dark
	Glow:       "#FFA600", // Profaned glow
	Frozen:     "#A0704A", // Cooled ember (inactive state)
}

// NightTheme is the night form of Providence: cyan, turquoise, icy blue.
var NightTheme = ThemeColors{
	Primary:    "#00DCFF", // Cyan flame
	Secondary:  "#40E0D0", // Turquoise
	Accent:     "#B0FFF0", // Greenish-white glow
	Background: "#0a0a14", // Void with blue tint
	Text:       "#c0d8e8", // Cool divine light
	Muted:      "#40556b", // Frost ash
	Success:    "#69F0AE", // Profaned teal
	Error:      "#FF6B6B", // Soft brimstone
	Warning:    "#B0FFF0", // Same as accent
	Border:     "#1a2535", // Dark frost
	Card:       "#0d1018", // Deep void
	Glow:       "#00DCFF", // Cyan glow
	Frozen:     "#4A708A", // Cooled frost
}

// ActiveTheme is the currently active theme. Set via ApplyTheme.
var ActiveTheme = FlameTheme

// currentThemeName tracks the name of the active theme for display.
var currentThemeName = "flame"

// --- Theme Color Vars (reassigned on theme switch) ---
// These are color.Color values used by lipgloss styles and gradients.

var (
	ColorPrimary    color.Color
	ColorSecondary  color.Color
	ColorAccent     color.Color
	ColorBackground color.Color
	ColorText       color.Color
	ColorMuted      color.Color
	ColorSuccess    color.Color
	ColorError      color.Color
	ColorWarning    color.Color
	ColorBorder     color.Color
	ColorCard       color.Color
	ColorGlow       color.Color
	ColorFrozen     color.Color
)

// --- Reusable Styles (reassigned on theme switch) ---

var (
	BannerStyle       lipgloss.Style
	BannerAccentStyle lipgloss.Style
	TabActiveStyle    lipgloss.Style
	TabInactiveStyle  lipgloss.Style
	StatusBarStyle    lipgloss.Style
	SelectedStyle     lipgloss.Style
	NormalStyle       lipgloss.Style
	MutedStyle        lipgloss.Style
	SuccessStyle      lipgloss.Style
	ErrorStyle        lipgloss.Style
	WarningStyle      lipgloss.Style
	AccentStyle       lipgloss.Style
	CardStyle         lipgloss.Style
	HeaderStyle       lipgloss.Style
	BorderStyle       lipgloss.Style
	TypeBadgeStyle    lipgloss.Style
	DividerStyle      lipgloss.Style
	MetaKeyStyle      lipgloss.Style
	MetaValueStyle    lipgloss.Style
	MetaPunctStyle    lipgloss.Style
	ScoreHighStyle    lipgloss.Style
	ScoreMedStyle     lipgloss.Style
	ScoreLowStyle     lipgloss.Style
	GlowStyle         lipgloss.Style
	UserMsgStyle      lipgloss.Style
	ToolNameStyle     lipgloss.Style
	ToolArgsStyle     lipgloss.Style
	ThinkingStyle     lipgloss.Style
	InputPrefixStyle  lipgloss.Style
)

func init() {
	ApplyTheme("flame")
}

// c converts a hex string to a color.Color via lipgloss.
func c(hex string) color.Color {
	return lipgloss.Color(hex)
}

// ApplyTheme switches the active theme and recomputes all color vars, styles, and gradients.
func ApplyTheme(name string) {
	switch name {
	case "night":
		ActiveTheme = NightTheme
		currentThemeName = "night"
	default:
		ActiveTheme = FlameTheme
		currentThemeName = "flame"
	}

	// Reassign all color vars from active theme.
	ColorPrimary = c(ActiveTheme.Primary)
	ColorSecondary = c(ActiveTheme.Secondary)
	ColorAccent = c(ActiveTheme.Accent)
	ColorBackground = c(ActiveTheme.Background)
	ColorText = c(ActiveTheme.Text)
	ColorMuted = c(ActiveTheme.Muted)
	ColorSuccess = c(ActiveTheme.Success)
	ColorError = c(ActiveTheme.Error)
	ColorWarning = c(ActiveTheme.Warning)
	ColorBorder = c(ActiveTheme.Border)
	ColorCard = c(ActiveTheme.Card)
	ColorGlow = c(ActiveTheme.Glow)
	ColorFrozen = c(ActiveTheme.Frozen)

	// Reassign all styles.
	reapplyStyles()

	// Recompute all precomputed gradients.
	recomputeFlameGradients()
	recomputeVizGradients()
	recomputeBannerGradient()
	recomputeSpinnerColors()
}

// reapplyStyles recreates all style vars from current color vars.
func reapplyStyles() {
	BannerStyle = lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Bold(true)

	BannerAccentStyle = lipgloss.NewStyle().
		Foreground(ColorSecondary)

	TabActiveStyle = lipgloss.NewStyle().
		Foreground(ColorBackground).
		Background(ColorPrimary).
		Bold(true).
		Padding(0, 1)

	TabInactiveStyle = lipgloss.NewStyle().
		Foreground(ColorMuted).
		Padding(0, 1)

	StatusBarStyle = lipgloss.NewStyle().
		Foreground(ColorMuted).
		PaddingTop(1)

	SelectedStyle = lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Bold(true)

	NormalStyle = lipgloss.NewStyle().
		Foreground(ColorText)

	MutedStyle = lipgloss.NewStyle().
		Foreground(ColorMuted)

	SuccessStyle = lipgloss.NewStyle().
		Foreground(ColorSuccess)

	ErrorStyle = lipgloss.NewStyle().
		Foreground(ColorError).
		Bold(true)

	WarningStyle = lipgloss.NewStyle().
		Foreground(ColorWarning)

	AccentStyle = lipgloss.NewStyle().
		Foreground(ColorAccent)

	CardStyle = lipgloss.NewStyle().
		Foreground(ColorCard)

	HeaderStyle = lipgloss.NewStyle().
		Foreground(ColorSecondary).
		Bold(true).
		PaddingBottom(1)

	BorderStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(ColorBorder).
		Padding(0, 1)

	TypeBadgeStyle = lipgloss.NewStyle().
		Foreground(ColorMuted).
		Border(lipgloss.NormalBorder()).
		BorderForeground(ColorBorder).
		BorderTop(false).
		BorderBottom(false).
		Bold(true).
		Padding(0, 1)

	DividerStyle = lipgloss.NewStyle().
		Foreground(ColorBorder)

	MetaKeyStyle = lipgloss.NewStyle().
		Foreground(ColorSecondary).
		Bold(true)

	MetaValueStyle = lipgloss.NewStyle().
		Foreground(ColorText)

	MetaPunctStyle = lipgloss.NewStyle().
		Foreground(ColorMuted)

	ScoreHighStyle = lipgloss.NewStyle().
		Foreground(ColorSuccess).
		Bold(true)

	ScoreMedStyle = lipgloss.NewStyle().
		Foreground(ColorWarning)

	ScoreLowStyle = lipgloss.NewStyle().
		Foreground(ColorError)

	GlowStyle = lipgloss.NewStyle().
		Foreground(ColorGlow).
		Bold(true)

	UserMsgStyle = lipgloss.NewStyle().
		Background(ColorCard).
		Foreground(ColorText).
		PaddingLeft(1).
		PaddingRight(1)

	ToolNameStyle = lipgloss.NewStyle().
		Foreground(ColorSecondary).
		Bold(true)

	ToolArgsStyle = lipgloss.NewStyle().
		Foreground(ColorMuted)

	ThinkingStyle = lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	InputPrefixStyle = lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Bold(true)

	// Update component styles (separate package, can't access theme directly).
	components.HintDescStyle = lipgloss.NewStyle().Foreground(ColorMuted)
	components.KeyCapStyle = lipgloss.NewStyle().
		Foreground(ColorBackground).
		Background(ColorSecondary).
		Bold(true).
		Padding(0, 1)
	components.SegmentStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(ColorBorder).
		Padding(0, 1).
		MarginRight(1)

	// Update text input / spinner theme colors in components.
	components.ThemePrimary = lipgloss.Color(ActiveTheme.Secondary)
	components.ThemeMuted = lipgloss.Color(ActiveTheme.Muted)
	components.ThemeText = lipgloss.Color(ActiveTheme.Text)
}

// Divider returns a horizontal line.
func Divider(width int) string {
	if width <= 0 {
		return ""
	}
	return DividerStyle.Render(strings.Repeat("\u2500", width))
}
