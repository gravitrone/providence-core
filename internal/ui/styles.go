package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// --- Theme Colors (Providence - The Profaned Goddess) ---

var (
	ColorPrimary    = lipgloss.Color("#FFA600") // Profaned amber
	ColorSecondary  = lipgloss.Color("#D77757") // Flame orange
	ColorAccent     = lipgloss.Color("#FFD700") // Holy gold
	ColorBackground = lipgloss.Color("#0a0a0a") // Void black
	ColorText       = lipgloss.Color("#e0d0c0") // Warm divine light
	ColorMuted      = lipgloss.Color("#6b5040") // Ember ash
	ColorSuccess    = lipgloss.Color("#19FA19") // Profaned emerald
	ColorError      = lipgloss.Color("#ff5555") // Brimstone red
	ColorWarning    = lipgloss.Color("#FFD700") // Holy gold
	ColorBorder     = lipgloss.Color("#3a2518") // Dark ember
	ColorCard       = lipgloss.Color("#1a1210") // Profaned dark
	ColorGlow       = lipgloss.Color("#FFA600") // Profaned glow
	ColorFrozen     = lipgloss.Color("#A0704A") // Cooled ember (inactive state)
)

// --- Reusable Styles ---

var (
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

	// Score-specific styles.
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

	// --- Agent Chat Styles (Claude Code inspired) ---

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
)

// Divider returns a horizontal line.
func Divider(width int) string {
	if width <= 0 {
		return ""
	}
	return DividerStyle.Render(strings.Repeat("─", width))
}
