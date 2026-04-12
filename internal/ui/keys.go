package ui

import "charm.land/bubbles/v2/key"

// KeyMap defines all keybindings for the providence TUI.
type KeyMap struct {
	Quit   key.Binding
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Filter key.Binding
	Scrape key.Binding
	Back   key.Binding
	Tab1   key.Binding
	Tab2   key.Binding
	TabL   key.Binding
	TabR   key.Binding
}

// DefaultKeyMap returns the default keybindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "quit"),
		),
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "detail"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		Scrape: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "scrape"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		Tab1: key.NewBinding(
			key.WithKeys("1"),
			key.WithHelp("1", "jobs tab"),
		),
		Tab2: key.NewBinding(
			key.WithKeys("2"),
			key.WithHelp("2", "agent tab"),
		),
		TabL: key.NewBinding(
			key.WithKeys("left"),
			key.WithHelp("←", "prev tab"),
		),
		TabR: key.NewBinding(
			key.WithKeys("right"),
			key.WithHelp("→", "next tab"),
		),
	}
}
