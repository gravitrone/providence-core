package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gravitrone/providence-core/internal/ui/components"
	"github.com/gravitrone/providence-core/internal/ui/dashboard"
	"github.com/gravitrone/providence-core/internal/ui/sidebar"
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

// --- Theme Color Vars ---

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

// --- Reusable Styles ---

var (
	BannerStyle       lipgloss.Style
	BannerAccentStyle lipgloss.Style
	TabActiveStyle    lipgloss.Style
	TabFocusStyle     lipgloss.Style
	TabTrailStyle     lipgloss.Style
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

	// Tool result display styles.
	ToolIconPendingStyle lipgloss.Style
	ToolIconSuccessStyle lipgloss.Style
	ToolIconErrorStyle   lipgloss.Style
	ToolContentLineStyle lipgloss.Style
	ToolContentBgColor   color.Color
	ToolErrorTagStyle    lipgloss.Style
	ToolErrorMsgStyle    lipgloss.Style
	ToolTruncationStyle  lipgloss.Style

	// Tool card gradient colors for frozen borders.
	ToolCardSuccessEdge color.Color
	ToolCardSuccessMid  color.Color
	ToolCardErrorEdge   color.Color
	ToolCardErrorMid    color.Color

	// Button styles for permission dialog.
	ButtonFocusStyle lipgloss.Style
	ButtonBlurStyle  lipgloss.Style
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

	reapplyStyles()
	recomputeFlameGradients()
	recomputeVizGradients()
	recomputeBannerGradient()
	recomputeSpinnerColors()

	dashboard.UpdateThemeColors(
		ActiveTheme.Primary,
		ActiveTheme.Secondary,
		ActiveTheme.Muted,
		ActiveTheme.Text,
	)

	sidebar.UpdateThemeColors(
		ActiveTheme.Primary,
		ActiveTheme.Secondary,
		ActiveTheme.Muted,
		ActiveTheme.Text,
		ActiveTheme.Border,
		ActiveTheme.Success,
		ActiveTheme.Error,
		ActiveTheme.Card,
	)
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

	TabFocusStyle = lipgloss.NewStyle().
		Foreground(ColorBackground).
		Background(ColorSecondary).
		Bold(true).
		Padding(0, 1)

	TabTrailStyle = lipgloss.NewStyle().
		Foreground(ColorSecondary).
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

	// Tool result display styles.
	ToolIconPendingStyle = lipgloss.NewStyle().Foreground(ColorMuted)
	ToolIconSuccessStyle = lipgloss.NewStyle().Foreground(c("#50C878"))
	ToolIconErrorStyle   = lipgloss.NewStyle().Foreground(c("#e05050"))
	ToolContentBgColor   = c("#141210") // slightly lighter than void black
	ToolContentLineStyle = lipgloss.NewStyle().
		Foreground(ColorMuted).
		Background(ToolContentBgColor)
	ToolErrorTagStyle = lipgloss.NewStyle().
		Background(c("#e05050")).
		Foreground(c("#ffffff")).
		Padding(0, 1)
	ToolErrorMsgStyle   = lipgloss.NewStyle().Foreground(ColorMuted)
	ToolTruncationStyle = lipgloss.NewStyle().Foreground(ColorMuted).Background(ToolContentBgColor)

	// Tool card frozen border gradient colors.
	ToolCardSuccessEdge = c(darkenHex("#50C878", 0.4))
	ToolCardSuccessMid = c("#50C878")
	ToolCardErrorEdge = c(darkenHex("#e05050", 0.4))
	ToolCardErrorMid = c("#e05050")

	// Button styles for permission dialog.
	ButtonFocusStyle = lipgloss.NewStyle().
		Foreground(c("#ffffff")).
		Background(ColorSecondary).
		Padding(0, 2)
	ButtonBlurStyle = lipgloss.NewStyle().
		Foreground(ColorMuted).
		Background(c("#1a1210")).
		Padding(0, 2)

	// Update component package styles (separate package, no direct theme access).
	components.HintDescStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#d0c8c0"))
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

	components.ThemePrimary = lipgloss.Color(ActiveTheme.Secondary)
	components.ThemeMuted = lipgloss.Color(ActiveTheme.Muted)
	components.ThemeText = lipgloss.Color(ActiveTheme.Text)
	components.AgentThemePrimaryHex = ActiveTheme.Secondary
	components.AgentThemeMutedHex = ActiveTheme.Muted
}

// --- Per-Engine Theme Identifiers ---

// EngineTheme maps an engine type to its visual identity. These are data-only
// definitions used by the TUI to determine colors and labels when switching
// between engine backends. Actual theme application is wired separately.
type EngineTheme struct {
	Name         string   // display name
	Primary      string   // primary color hex
	Secondary    string   // secondary color hex
	BannerText   string   // what the banner says
	SpinnerVerbs []string // engine-specific activity verbs
}

// EngineThemes maps engine type strings to their visual identities.
var EngineThemes = map[string]EngineTheme{
	"native": {
		Name: "The Profaned Core", Primary: "#FFA600", Secondary: "#D77757",
		BannerText:   "PROVIDENCE",
		SpinnerVerbs: []string{"consecrating", "invoking", "channeling"},
	},
	"claude": {
		Name: "Claude Code Embedded", Primary: "#CF6E22", Secondary: "#A0704A",
		BannerText:   "CLAUDE CODE",
		SpinnerVerbs: []string{"thinking", "analyzing", "reasoning"},
	},
	"codex_headless": {
		Name: "Codex Engine", Primary: "#00FF88", Secondary: "#00CC66",
		BannerText:   "CODEX",
		SpinnerVerbs: []string{"computing", "generating", "processing"},
	},
	"opencode": {
		Name: "OpenCode Engine", Primary: "#7B68EE", Secondary: "#6A5ACD",
		BannerText:   "OPENCODE",
		SpinnerVerbs: []string{"serving", "streaming", "responding"},
	},
}

// Divider returns a horizontal line.
func Divider(width int) string {
	if width <= 0 {
		return ""
	}
	return DividerStyle.Render(strings.Repeat("\u2500", width))
}

// renderVerticalDivider renders a single-character-wide vertical line of the
// given height, using the provided border color hex.
func renderVerticalDivider(height int, borderHex string) string {
	if height <= 0 {
		return ""
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(borderHex))
	lines := make([]string, height)
	for i := range lines {
		lines[i] = style.Render("\u2502") // │
	}
	return strings.Join(lines, "\n")
}
