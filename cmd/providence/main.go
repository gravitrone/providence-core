package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/gravitrone/providence-core/internal/ui"
)

// Main runs the CLI entrypoint.
func main() {
	root := newRootCommand()
	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// NewRootCommand builds the root cobra command.
func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "providence",
		Short: "The Profaned Core - autonomous AI harness",
		Long:  "providence: autonomous AI harness - terminal, web, and beyond.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runTUI()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	return root
}

// RunBubbleTUI is a var so tests can override it without launching a real program.
var runBubbleTUI = func(app tea.Model) error {
	p := tea.NewProgram(app)
	_, err := p.Run()
	return err
}

// RunTUI launches the fullscreen Bubble Tea TUI.
func runTUI() error {
	if !isInteractiveTerminal(os.Stdout) {
		fmt.Println(ui.RenderBanner())
		return nil
	}
	app := ui.NewApp()
	if err := runBubbleTUI(app); err != nil {
		return fmt.Errorf("tui error: %w", err)
	}
	return nil
}

// IsInteractiveTerminal reports whether file is an interactive TTY.
func isInteractiveTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// Init sets up environment defaults.
func init() {
	_ = os.Setenv("COLORTERM", "truecolor")
}
