package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/gravitrone/providence-core/internal/config"
	"github.com/gravitrone/providence-core/internal/engine"
	_ "github.com/gravitrone/providence-core/internal/engine/claude"
	_ "github.com/gravitrone/providence-core/internal/engine/direct"
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

// headlessFlag enables headless NDJSON mode.
var headlessFlag bool

// NewRootCommand builds the root cobra command.
func newRootCommand() *cobra.Command {
	cwd, _ := os.Getwd()
	cfg := config.LoadMerged(cwd)

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
			if headlessFlag {
				st, err := store.Open(store.DefaultDBPath())
				if err != nil {
					fmt.Fprintf(os.Stderr, "warning: session db: %v\n", err)
				}
				if st != nil {
					defer st.Close()
				}
				return runHeadless(engineFlag, cfg, st)
			}
			return runTUI(engineFlag, cfg)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.Flags().StringVar(&engineFlag, "engine", engineDefault, "AI engine backend (claude, direct, openai)")
	root.Flags().BoolVar(&headlessFlag, "headless", false, "Run in headless NDJSON mode")

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

// --- Headless NDJSON Mode ---

// headlessInitEvent is emitted on stdout when headless mode starts.
type headlessInitEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Model   string `json:"model,omitempty"`
	Engine  string `json:"engine,omitempty"`
}

// headlessUserMsg is the expected input format on stdin.
type headlessUserMsg struct {
	Type    string `json:"type"`
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

// headlessOutputEvent wraps engine events for NDJSON output.
type headlessOutputEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

// runHeadless runs the engine in headless NDJSON mode (stdin -> engine -> stdout).
func runHeadless(engineType string, cfg config.Config, _ *store.Store) error {
	wd, _ := os.Getwd()

	eng, err := engine.NewEngine(engine.EngineConfig{
		Type:         engine.EngineType(engineType),
		SystemPrompt: engine.BuildSystemPrompt(nil),
		Model:        cfg.Model,
		APIKey:       os.Getenv("ANTHROPIC_API_KEY"),
		WorkDir:      wd,
	})
	if err != nil {
		return fmt.Errorf("failed to create engine: %w", err)
	}
	defer eng.Close()

	enc := json.NewEncoder(os.Stdout)

	// Emit init event.
	_ = enc.Encode(headlessInitEvent{
		Type:    "system",
		Subtype: "init",
		Model:   cfg.Model,
		Engine:  engineType,
	})

	scanner := bufio.NewScanner(os.Stdin)
	// Allow large messages (1MB).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var msg headlessUserMsg
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			_ = enc.Encode(headlessOutputEvent{
				Type:  "error",
				Error: "invalid json: " + err.Error(),
			})
			continue
		}

		if msg.Type != "user" || msg.Message.Content == "" {
			continue
		}

		if err := eng.Send(msg.Message.Content); err != nil {
			_ = enc.Encode(headlessOutputEvent{
				Type:  "error",
				Error: "send failed: " + err.Error(),
			})
			continue
		}

		// Drain events until result.
		for event := range eng.Events() {
			if event.Err != nil {
				_ = enc.Encode(headlessOutputEvent{
					Type:  "error",
					Error: event.Err.Error(),
				})
				continue
			}
			_ = enc.Encode(headlessOutputEvent{
				Type:    event.Type,
				Content: event.Raw,
			})
			if event.Type == "result" {
				break
			}
		}
	}

	return scanner.Err()
}
