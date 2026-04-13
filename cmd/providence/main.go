package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/gravitrone/providence-core/internal/config"
	"github.com/gravitrone/providence-core/internal/engine/plugin"
	"github.com/gravitrone/providence-core/internal/store"
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

// engineFlag holds the --engine flag value.
var engineFlag string

// NewRootCommand builds the root cobra command.
func newRootCommand() *cobra.Command {
	cfg := config.Load()

	// Default engine from config, fallback to "claude".
	engineDefault := "claude"
	if cfg.Engine != "" {
		engineDefault = cfg.Engine
	}

	root := &cobra.Command{
		Use:   "providence",
		Short: "The Profaned Core - autonomous AI harness",
		Long:  "providence: autonomous AI harness - terminal, web, and beyond.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runTUI(engineFlag, cfg)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.Flags().StringVar(&engineFlag, "engine", engineDefault, "AI engine backend (claude, direct, openai)")

	root.RegisterFlagCompletionFunc("engine", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"claude", "direct"}, cobra.ShellCompDirectiveNoFileComp
	})

	completionCmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish]",
		Short: "Generate shell completion script",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(os.Stdout)
			case "zsh":
				return root.GenZshCompletion(os.Stdout)
			case "fish":
				return root.GenFishCompletion(os.Stdout, true)
			default:
				return fmt.Errorf("unsupported shell: %s", args[0])
			}
		},
	}
	root.AddCommand(completionCmd)

	return root
}

// RunBubbleTUI is a var so tests can override it without launching a real program.
var runBubbleTUI = func(app tea.Model) error {
	p := tea.NewProgram(app)
	_, err := p.Run()
	return err
}

// RunTUI launches the fullscreen Bubble Tea TUI.
func runTUI(engineType string, cfg config.Config) error {
	if !isInteractiveTerminal(os.Stdout) {
		fmt.Println(ui.RenderBanner())
		return nil
	}
	st, err := store.Open(store.DefaultDBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: session db: %v\n", err)
	}
	if st != nil {
		defer st.Close()
	}
	// Initialize plugin manager.
	homeDir, _ := os.UserHomeDir()
	pluginDir := filepath.Join(homeDir, ".providence", "plugins")
	pluginMgr := plugin.NewManager(pluginDir)
	if err := pluginMgr.LoadAll(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: plugin load: %v\n", err)
	}

	app := ui.NewApp(engineType, cfg, st)
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
