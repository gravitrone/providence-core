package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/gravitrone/providence-core/internal/config"
	"github.com/gravitrone/providence-core/internal/engine"
	_ "github.com/gravitrone/providence-core/internal/engine/claude"
	_ "github.com/gravitrone/providence-core/internal/engine/codex_headless"
	_ "github.com/gravitrone/providence-core/internal/engine/direct"
	"github.com/gravitrone/providence-core/internal/engine/ember"
	"github.com/gravitrone/providence-core/internal/engine/headless"
	"github.com/gravitrone/providence-core/internal/engine/plugin"
	"github.com/gravitrone/providence-core/internal/overlay"
	"github.com/gravitrone/providence-core/internal/store"
	"github.com/gravitrone/providence-core/internal/ui"
)

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

// outputFormat and inputFormat are CC-compat aliases for --headless.
var outputFormat string
var inputFormat string

// resumeFlag takes a session ID or title substring to resume.
var resumeFlag string

// continueFlag loads the most recent session.
var continueFlag bool

// newRootCommand builds the root cobra command.
func newRootCommand() *cobra.Command {
	cwd, _ := os.Getwd()
	cfg := config.LoadMerged(cwd)

	// Load .claude/settings.json for CC compatibility (env vars, tool permissions, hooks).
	if claudeSettings, err := config.LoadClaudeSettings(cwd); err != nil {
		fmt.Fprintf(os.Stderr, "warning: .claude/settings.json: %v\n", err)
	} else if claudeSettings != nil {
		// Merge hooks from settings.json (lower priority than TOML config).
		settingsHooks := claudeSettings.ParseHooks()
		if len(cfg.Hooks.ToMap()) == 0 {
			cfg.Hooks = settingsHooks
		}
	}

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
			// --output-format stream-json and --input-format stream-json are CC-compat aliases.
			if outputFormat == "stream-json" || inputFormat == "stream-json" {
				headlessFlag = true
			}
			if headlessFlag {
				return runHeadless(engineFlag, cfg)
			}
			return runTUI(engineFlag, cfg, resumeFlag, continueFlag)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.Flags().StringVar(&engineFlag, "engine", engineDefault, "AI engine backend (claude, direct, openai)")
	root.Flags().BoolVar(&headlessFlag, "headless", false, "Run in headless NDJSON mode")
	root.Flags().StringVar(&outputFormat, "output-format", "", "Output format (stream-json enables headless)")
	root.Flags().StringVar(&inputFormat, "input-format", "", "Input format (stream-json enables headless)")
	root.Flags().StringVar(&resumeFlag, "resume", "", "Resume session by ID or title substring")
	root.Flags().BoolVar(&continueFlag, "continue", false, "Resume the most recent session")

	root.RegisterFlagCompletionFunc("engine", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"claude", "direct", "codex_headless", "codex_headless"}, cobra.ShellCompDirectiveNoFileComp
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

// runBubbleTUI is a var so tests can override it without launching a real program.
var runBubbleTUI = func(app tea.Model) error {
	p := tea.NewProgram(app)
	_, err := p.Run()
	return err
}

// runTUI launches the fullscreen Bubble Tea TUI.
func runTUI(engineType string, cfg config.Config, resumeQuery string, continueSession bool) error {
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
		// Clean up sessions older than 30 days on startup.
		if cleaned, cerr := st.CleanupOldSessions(30); cerr != nil {
			fmt.Fprintf(os.Stderr, "warning: session cleanup: %v\n", cerr)
		} else if cleaned > 0 {
			fmt.Fprintf(os.Stderr, "cleaned %d old session(s)\n", cleaned)
		}
	}

	// Resolve --resume / --continue to a ResumeData payload.
	var resume *ui.ResumeData
	if st != nil && (resumeQuery != "" || continueSession) {
		resume, err = resolveResumeSession(st, resumeQuery, continueSession)
		if err != nil {
			return err
		}
	}

	// Initialize plugin manager.
	homeDir, _ := os.UserHomeDir()
	pluginDir := filepath.Join(homeDir, ".providence", "plugins")
	pluginMgr := plugin.NewManager(pluginDir)
	if err := pluginMgr.LoadAll(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: plugin load: %v\n", err)
	}

	// Wire overlay manager + bridge if overlay is enabled in config.
	var appOpts []ui.AppOption
	if cfg.Overlay.Enable {
		emberState := ember.New()
		logger := slog.Default()

		overlayCfg := overlay.Config{
			SocketPath:  cfg.Overlay.SocketPath,
			BinaryPath:  cfg.Overlay.BinaryPath,
			AutoStart:   cfg.Overlay.AutoStart,
			Spawn:       cfg.Overlay.Spawn,
			ExcludeApps: cfg.Overlay.ExcludeApps,
		}
		overlayMgr := overlay.NewManager(overlayCfg, logger)
		overlayBridge := overlay.NewBridgeWithMode(nil, emberState, nil, overlayMgr, logger, cfg.Overlay.ContextInjection)
		// Phase 10: advertise runtime prefs (TTS, panel position, excluded apps)
		// to the overlay in each Welcome.
		overlayBridge.SetRuntimePrefs(
			cfg.Overlay.TTSEnabled,
			cfg.Overlay.Position,
			cfg.Overlay.ExcludeApps,
			cfg.Overlay.UIMode,
			cfg.Overlay.ChatHistoryLimit,
			cfg.Overlay.ChatAlpha,
			cfg.Overlay.ChatPosition,
		)

		overlayMgr.SetCallbacks(
			func() {
				if !emberState.Active {
					emberState.Activate()
				}
			},
			func() {
				// Symmetric cleanup on overlay stop.
			},
		)

		appOpts = append(appOpts, ui.WithOverlay(overlayMgr, overlayBridge))

		if cfg.Overlay.AutoStart {
			// Use Background context so the server lives for the lifetime of
			// the TUI. A goroutine-local WithCancel + defer cancel would tear
			// the server down the instant Start returns (which happens fast
			// in spawn=false mode).
			go func() {
				if err := overlayMgr.Start(context.Background(), overlayBridge); err != nil {
					fmt.Fprintf(os.Stderr, "warning: overlay auto-start: %v\n", err)
				}
			}()
		}
	}

	app := ui.NewApp(engineType, cfg, st, resume, appOpts...)
	if err := runBubbleTUI(app); err != nil {
		return fmt.Errorf("tui error: %w", err)
	}
	return nil
}

// resolveResumeSession looks up a session from the store based on the CLI flags.
// --resume takes a session ID or title substring; --continue picks the most
// recent session. Returns nil if no session was found.
func resolveResumeSession(st *store.Store, query string, mostRecent bool) (*ui.ResumeData, error) {
	var sess *store.SessionRow
	var err error

	if query != "" {
		sess, err = st.FindSessionByIDOrTitle(query)
		if err != nil {
			return nil, fmt.Errorf("session lookup failed: %w", err)
		}
		if sess == nil {
			return nil, fmt.Errorf("no session matching %q", query)
		}
	} else if mostRecent {
		sess, err = st.MostRecentSession()
		if err != nil {
			return nil, fmt.Errorf("session lookup failed: %w", err)
		}
		if sess == nil {
			return nil, fmt.Errorf("no sessions found")
		}
	}

	if sess == nil {
		return nil, nil
	}

	msgs, err := st.GetMessages(sess.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load session messages: %w", err)
	}

	return &ui.ResumeData{
		SessionID: sess.ID,
		Title:     sess.Title,
		Messages:  msgs,
	}, nil
}

// isInteractiveTerminal reports whether file is an interactive TTY.
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

// init sets up environment defaults.
func init() {
	_ = os.Setenv("COLORTERM", "truecolor")
}

// --- Headless NDJSON Mode ---

// runHeadless runs the engine in headless NDJSON mode via headless.Server.
func runHeadless(engineType string, cfg config.Config) error {
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

	srv := headless.NewServer(eng, os.Stdin, os.Stdout, cfg.Model, engineType)

	// Wire up signal handling for clean shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	return srv.Run(ctx)
}
