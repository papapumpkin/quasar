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

	"github.com/aaronsalm/quasar/internal/agent"
	"github.com/aaronsalm/quasar/internal/beads"
	"github.com/aaronsalm/quasar/internal/claude"
	"github.com/aaronsalm/quasar/internal/config"
	"github.com/aaronsalm/quasar/internal/loop"
	"github.com/aaronsalm/quasar/internal/ui"
	"github.com/spf13/cobra"
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

	// Apply flag overrides.
	if v, _ := cmd.Flags().GetInt("max-cycles"); v > 0 {
		cfg.MaxReviewCycles = v
	}
	if v, _ := cmd.Flags().GetFloat64("max-budget"); v > 0 {
		cfg.MaxBudgetUSD = v
	}
	if v, _ := cmd.Flags().GetBool("verbose"); v {
		cfg.Verbose = true
	}

	// Load custom prompts from files if specified.
	coderPrompt := agent.DefaultCoderSystemPrompt
	if cfg.CoderSystemPrompt != "" {
		coderPrompt = cfg.CoderSystemPrompt
	}
	if f, _ := cmd.Flags().GetString("coder-prompt-file"); f != "" {
		data, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("failed to read coder prompt file: %w", err)
		}
		coderPrompt = string(data)
	}

	reviewerPrompt := agent.DefaultReviewerSystemPrompt
	if cfg.ReviewerSystemPrompt != "" {
		reviewerPrompt = cfg.ReviewerSystemPrompt
	}
	if f, _ := cmd.Flags().GetString("reviewer-prompt-file"); f != "" {
		data, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("failed to read reviewer prompt file: %w", err)
		}
		reviewerPrompt = string(data)
	}

	// Validate dependencies.
	claudeInv := &claude.Invoker{ClaudePath: cfg.ClaudePath, Verbose: cfg.Verbose}
	if err := claudeInv.Validate(); err != nil {
		printer.Error(fmt.Sprintf("claude not available: %v", err))
		return err
	}

	beadsClient := &beads.Client{BeadsPath: cfg.BeadsPath, Verbose: cfg.Verbose}
	if err := beadsClient.Validate(); err != nil {
		printer.Error(fmt.Sprintf("beads not available: %v", err))
		return err
	}

	// Resolve working directory.
	workDir := cfg.WorkDir
	if workDir == "." || workDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
		workDir = wd
	}

	taskLoop := &loop.Loop{
		Invoker:      claudeInv,
		Beads:        beadsClient,
		UI:           printer,
		MaxCycles:    cfg.MaxReviewCycles,
		MaxBudgetUSD: cfg.MaxBudgetUSD,
		Model:        cfg.Model,
		CoderPrompt:  coderPrompt,
		ReviewPrompt: reviewerPrompt,
		WorkDir:      workDir,
	}

	// Context with signal handling.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		printer.Info("\nshutting down...")
		cancel()
	}()

	// Auto mode: read from args or stdin, run once, exit.
	if auto, _ := cmd.Flags().GetBool("auto"); auto {
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

	// Interactive REPL.
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
	err := taskLoop.RunTask(ctx, task)
	if err == nil {
		return nil
	}

	if errors.Is(err, loop.ErrMaxCycles) || errors.Is(err, loop.ErrBudgetExceeded) {
		// These are expected termination conditions, not fatal.
		return err
	}

	if ctx.Err() != nil {
		printer.Info("task cancelled")
		return err
	}

	printer.Error(err.Error())
	return err
}
