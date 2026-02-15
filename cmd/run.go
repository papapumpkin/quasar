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

	"github.com/aaronsalm/quasar/internal/agent"
	"github.com/aaronsalm/quasar/internal/beads"
	"github.com/aaronsalm/quasar/internal/claude"
	"github.com/aaronsalm/quasar/internal/config"
	"github.com/aaronsalm/quasar/internal/loop"
	"github.com/aaronsalm/quasar/internal/ui"
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

	taskLoop, err := buildLoop(&cfg, printer, coderPrompt, reviewerPrompt)
	if err != nil {
		return err
	}

	ctx, cancel := setupSignalContext(printer)
	defer cancel()

	if auto, _ := cmd.Flags().GetBool("auto"); auto {
		return runAutoMode(ctx, taskLoop, printer, args)
	}
	return runREPL(ctx, taskLoop, printer, &cfg)
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
func buildLoop(cfg *config.Config, printer *ui.Printer, coderPrompt, reviewerPrompt string) (*loop.Loop, error) {
	claudeInv := &claude.Invoker{ClaudePath: cfg.ClaudePath, Verbose: cfg.Verbose}
	if err := claudeInv.Validate(); err != nil {
		printer.Error(fmt.Sprintf("claude not available: %v", err))
		return nil, err
	}

	beadsClient := &beads.Client{BeadsPath: cfg.BeadsPath, Verbose: cfg.Verbose}
	if err := beadsClient.Validate(); err != nil {
		printer.Error(fmt.Sprintf("beads not available: %v", err))
		return nil, err
	}

	workDir, err := resolveWorkDir(cfg.WorkDir)
	if err != nil {
		return nil, err
	}

	return &loop.Loop{
		Invoker:      claudeInv,
		Beads:        beadsClient,
		UI:           printer,
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

// runAutoMode reads a task from args or stdin and runs it once.
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
