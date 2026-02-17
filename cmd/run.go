package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/beads"
	"github.com/papapumpkin/quasar/internal/claude"
	"github.com/papapumpkin/quasar/internal/config"
	"github.com/papapumpkin/quasar/internal/loop"
	"github.com/papapumpkin/quasar/internal/tui"
	"github.com/papapumpkin/quasar/internal/ui"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start the interactive coder-reviewer REPL",
	RunE:  runRun,
}

func init() {
	runCmd.Flags().Int("max-cycles", 0, "override max review cycles")
	runCmd.Flags().Float64("max-budget", 0, "override max budget in USD")
	runCmd.Flags().String("coder-prompt-file", "", "file containing custom coder system prompt")
	runCmd.Flags().String("reviewer-prompt-file", "", "file containing custom reviewer system prompt")
	runCmd.Flags().Bool("auto", false, "run a single task from stdin and exit (non-interactive)")
	runCmd.Flags().Bool("no-tui", false, "disable TUI even on a TTY (use stderr printer)")

	rootCmd.AddCommand(runCmd)
}

func runRun(cmd *cobra.Command, args []string) error {
	cfg := config.Load()
	printer := ui.New()

	applyFlagOverrides(cmd, &cfg)

	coderPrompt, reviewerPrompt, err := loadPrompts(cmd, &cfg)
	if err != nil {
		return err
	}

	auto, _ := cmd.Flags().GetBool("auto")
	noTUI, _ := cmd.Flags().GetBool("no-tui")

	// TUI path: auto mode on a TTY without --no-tui.
	if auto && !noTUI && isStderrTTY() {
		return runAutoTUI(cfg, printer, coderPrompt, reviewerPrompt, args)
	}

	taskLoop, err := buildLoop(&cfg, printer, coderPrompt, reviewerPrompt)
	if err != nil {
		return err
	}

	ctx, cancel := setupSignalContext(printer)
	defer cancel()

	if auto {
		return runAutoMode(ctx, taskLoop, printer, args)
	}
	return runREPL(ctx, taskLoop, printer, &cfg)
}

// runAutoTUI launches the BubbleTea TUI for a single auto-mode task.
func runAutoTUI(cfg config.Config, printer *ui.Printer, coderPrompt, reviewerPrompt string, args []string) error {
	task := strings.Join(args, " ")
	if task == "" {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			task = scanner.Text()
		}
	}
	if task == "" {
		return fmt.Errorf("no task provided in auto mode")
	}

	workDir, err := resolveWorkDir(cfg.WorkDir)
	if err != nil {
		return err
	}

	p := tui.NewProgram(tui.ModeLoop)
	bridge := tui.NewUIBridge(p, workDir)

	taskLoop, err := buildLoop(&cfg, bridge, coderPrompt, reviewerPrompt)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run the loop in a background goroutine; report completion to the TUI.
	go func() {
		_, loopErr := taskLoop.RunTask(ctx, task)
		p.Send(tui.MsgLoopDone{Err: loopErr})
	}()

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	// After TUI exits, report result to stderr.
	if m, ok := finalModel.(tui.AppModel); ok && m.DoneErr != nil {
		if !errors.Is(m.DoneErr, loop.ErrMaxCycles) && !errors.Is(m.DoneErr, loop.ErrBudgetExceeded) {
			printer.Error(m.DoneErr.Error())
		}
		return m.DoneErr
	}
	return nil
}

// applyFlagOverrides applies CLI flag values to the loaded config.
func applyFlagOverrides(cmd *cobra.Command, cfg *config.Config) {
	if v, _ := cmd.Flags().GetInt("max-cycles"); v > 0 {
		cfg.MaxReviewCycles = v
	}
	if v, _ := cmd.Flags().GetFloat64("max-budget"); v > 0 {
		cfg.MaxBudgetUSD = v
	}
	if v, _ := cmd.Flags().GetBool("verbose"); v {
		cfg.Verbose = true
	}
}

// loadPrompts resolves coder and reviewer system prompts from config and flag overrides.
func loadPrompts(cmd *cobra.Command, cfg *config.Config) (coder, reviewer string, err error) {
	coder = agent.DefaultCoderSystemPrompt
	if cfg.CoderSystemPrompt != "" {
		coder = cfg.CoderSystemPrompt
	}
	if f, _ := cmd.Flags().GetString("coder-prompt-file"); f != "" {
		data, err := os.ReadFile(f)
		if err != nil {
			return "", "", fmt.Errorf("failed to read coder prompt file: %w", err)
		}
		coder = string(data)
	}

	reviewer = agent.DefaultReviewerSystemPrompt
	if cfg.ReviewerSystemPrompt != "" {
		reviewer = cfg.ReviewerSystemPrompt
	}
	if f, _ := cmd.Flags().GetString("reviewer-prompt-file"); f != "" {
		data, err := os.ReadFile(f)
		if err != nil {
			return "", "", fmt.Errorf("failed to read reviewer prompt file: %w", err)
		}
		reviewer = string(data)
	}
	return coder, reviewer, nil
}

// buildLoop validates dependencies, resolves the working directory, and
// constructs a Loop ready to execute tasks.
func buildLoop(cfg *config.Config, uiHandler ui.UI, coderPrompt, reviewerPrompt string) (*loop.Loop, error) {
	claudeInv := &claude.Invoker{ClaudePath: cfg.ClaudePath, Verbose: cfg.Verbose}
	if err := claudeInv.Validate(); err != nil {
		uiHandler.Error(fmt.Sprintf("claude not available: %v", err))
		return nil, err
	}

	beadsClient := &beads.CLI{BeadsPath: cfg.BeadsPath, Verbose: cfg.Verbose}
	if err := beadsClient.Validate(); err != nil {
		uiHandler.Error(fmt.Sprintf("beads not available: %v", err))
		return nil, err
	}

	workDir, err := resolveWorkDir(cfg.WorkDir)
	if err != nil {
		return nil, err
	}

	git := loop.NewCycleCommitter(context.Background(), workDir)

	return &loop.Loop{
		Invoker:      claudeInv,
		Beads:        beadsClient,
		UI:           uiHandler,
		Git:          git,
		MaxCycles:    cfg.MaxReviewCycles,
		MaxBudgetUSD: cfg.MaxBudgetUSD,
		Model:        cfg.Model,
		CoderPrompt:  coderPrompt,
		ReviewPrompt: reviewerPrompt,
		WorkDir:      workDir,
	}, nil
}

// resolveWorkDir returns an absolute working directory path.
func resolveWorkDir(workDir string) (string, error) {
	if workDir != "" && workDir != "." {
		return workDir, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}
	return wd, nil
}

// isStderrTTY reports whether stderr is connected to a terminal.
func isStderrTTY() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// setupSignalContext returns a context that is canceled on SIGINT or SIGTERM.
func setupSignalContext(printer *ui.Printer) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		printer.Info("\nshutting down...")
		cancel()
	}()
	return ctx, cancel
}

// runAutoMode reads a task from args or stdin and runs it once (stderr printer path).
func runAutoMode(ctx context.Context, taskLoop *loop.Loop, printer *ui.Printer, args []string) error {
	task := strings.Join(args, " ")
	if task == "" {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			task = scanner.Text()
		}
	}
	if task == "" {
		return fmt.Errorf("no task provided in auto mode")
	}
	return runTask(ctx, taskLoop, printer, task)
}

// runREPL starts the interactive read-eval-print loop.
func runREPL(ctx context.Context, taskLoop *loop.Loop, printer *ui.Printer, cfg *config.Config) error {
	printer.Banner()
	printer.Info("type a task, 'help', 'status', or 'quit'")
	printer.ShowStatus(cfg.MaxReviewCycles, cfg.MaxBudgetUSD, cfg.Model)
	fmt.Fprintln(os.Stderr)

	scanner := bufio.NewScanner(os.Stdin)
	for {
		printer.Prompt()
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		switch strings.ToLower(input) {
		case "quit", "exit", "q":
			printer.Info("goodbye")
			return nil
		case "help", "h", "?":
			printer.ShowHelp()
			continue
		case "status":
			printer.ShowStatus(cfg.MaxReviewCycles, cfg.MaxBudgetUSD, cfg.Model)
			continue
		}

		if err := runTask(ctx, taskLoop, printer, input); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			// Non-fatal errors: continue the REPL.
		}
		fmt.Fprintln(os.Stderr)
	}
	return nil
}

func runTask(ctx context.Context, taskLoop *loop.Loop, printer *ui.Printer, task string) error {
	_, err := taskLoop.RunTask(ctx, task)
	if err == nil {
		return nil
	}

	if errors.Is(err, loop.ErrMaxCycles) || errors.Is(err, loop.ErrBudgetExceeded) {
		// These are expected termination conditions, not fatal.
		return err
	}

	if ctx.Err() != nil {
		printer.Info("task canceled")
		return err
	}

	printer.Error(err.Error())
	return err
}
