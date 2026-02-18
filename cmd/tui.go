package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/beads"
	"github.com/papapumpkin/quasar/internal/claude"
	"github.com/papapumpkin/quasar/internal/config"
	"github.com/papapumpkin/quasar/internal/loop"
	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/tui"
	"github.com/papapumpkin/quasar/internal/ui"
)

// tuiCmd launches the TUI home screen for browsing and running nebulas.
var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch the interactive TUI home screen",
	Long: `Launch the Quasar TUI home screen that auto-discovers nebulas in
the .nebulas/ directory of the current (or specified) directory. From the
landing page you can browse nebulas, see their status, and select one to
run.`,
	Args: cobra.NoArgs,
	RunE: runTUI,
}

func init() {
	tuiCmd.Flags().String("dir", "", "directory to scan for .nebulas/ (default: cwd)")
	tuiCmd.Flags().Bool("no-splash", false, "skip the startup splash animation")
	tuiCmd.Flags().Int("max-workers", 1, "maximum concurrent workers")
	rootCmd.AddCommand(tuiCmd)
}

// runTUI discovers nebulas in .nebulas/ and launches the home-to-execution loop.
func runTUI(cmd *cobra.Command, _ []string) error {
	printer := ui.New()

	// Determine the base directory to scan.
	baseDir, _ := cmd.Flags().GetString("dir")
	if baseDir == "" {
		var err error
		baseDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	nebulaeDir := filepath.Join(baseDir, ".nebulas")
	if _, err := os.Stat(nebulaeDir); os.IsNotExist(err) {
		return fmt.Errorf("no .nebulas/ directory found in %s", baseDir)
	} else if err != nil {
		return fmt.Errorf("failed to access %s: %w", nebulaeDir, err)
	}

	if !isStderrTTY() {
		return fmt.Errorf("quasar tui requires a TTY (terminal)")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if v, _ := cmd.Flags().GetBool("verbose"); v {
		cfg.Verbose = true
	}

	noSplash, _ := cmd.Flags().GetBool("no-splash")
	maxWorkers, _ := cmd.Flags().GetInt("max-workers")
	maxWorkersExplicit := cmd.Flags().Changed("max-workers")

	// Home-to-execution loop: discover → select → run → repeat.
	for {
		choices, discoverErr := tui.DiscoverAllNebulae(nebulaeDir)
		if discoverErr != nil {
			printer.Error(fmt.Sprintf("failed to discover nebulas: %v", discoverErr))
			return discoverErr
		}

		homeProgram := tui.NewHomeProgram(nebulaeDir, choices, noSplash)
		finalModel, tuiErr := homeProgram.Run()
		if tuiErr != nil {
			return fmt.Errorf("TUI error: %w", tuiErr)
		}

		appModel, ok := finalModel.(tui.AppModel)
		if !ok {
			return nil
		}

		// If no nebula was selected (user quit), exit cleanly.
		selectedDir := appModel.SelectedNebula
		if selectedDir == "" {
			return nil
		}

		// Run the selected nebula.
		runErr := runSelectedNebula(cfg, printer, selectedDir, noSplash, maxWorkers, maxWorkersExplicit)
		if runErr != nil {
			printer.Error(fmt.Sprintf("nebula execution error: %v", runErr))
			// Don't exit — return to the home screen.
		}

		// After splash is shown once, skip it on subsequent iterations.
		noSplash = true
	}
}

// runSelectedNebula loads, validates, and executes a single nebula in TUI mode.
// It reuses the same setup logic as runNebulaApply's TUI path.
// maxWorkersExplicit indicates whether the user explicitly set --max-workers;
// when false, the nebula manifest's MaxWorkers value takes precedence.
func runSelectedNebula(cfg config.Config, printer *ui.Printer, dir string, noSplash bool, maxWorkers int, maxWorkersExplicit bool) error {
	n, err := nebula.Load(dir)
	if err != nil {
		return fmt.Errorf("failed to load nebula: %w", err)
	}

	if errs := nebula.Validate(n); len(errs) > 0 {
		printer.NebulaValidateResult(n.Manifest.Nebula.Name, len(n.Phases), errs)
		return fmt.Errorf("validation failed")
	}

	state, err := nebula.LoadState(dir)
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	client := &beads.CLI{BeadsPath: cfg.BeadsPath, Verbose: cfg.Verbose}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	plan, err := nebula.BuildPlan(ctx, n, state, client)
	if err != nil {
		return fmt.Errorf("failed to build plan: %w", err)
	}

	if !plan.HasChanges() {
		printer.Info("nothing to do — all phases already applied")
		return nil
	}

	if err := nebula.Apply(ctx, plan, n, state, client); err != nil {
		return fmt.Errorf("failed to apply plan: %w", err)
	}

	// If --max-workers was not explicitly set, use nebula execution config.
	if maxWorkers <= 0 {
		maxWorkers = 1
	}
	if !maxWorkersExplicit && n.Manifest.Execution.MaxWorkers > 0 {
		maxWorkers = n.Manifest.Execution.MaxWorkers
	}

	// Load custom prompts.
	coderPrompt := agent.DefaultCoderSystemPrompt
	if cfg.CoderSystemPrompt != "" {
		coderPrompt = cfg.CoderSystemPrompt
	}
	reviewerPrompt := agent.DefaultReviewerSystemPrompt
	if cfg.ReviewerSystemPrompt != "" {
		reviewerPrompt = cfg.ReviewerSystemPrompt
	}

	claudeInv := claude.NewInvoker(cfg.ClaudePath, cfg.Verbose)
	if err := claudeInv.Validate(); err != nil {
		return fmt.Errorf("claude not available: %w", err)
	}

	workDir := cfg.WorkDir
	if n.Manifest.Context.WorkingDir != "" {
		workDir = n.Manifest.Context.WorkingDir
	}
	if workDir == "." || workDir == "" {
		wd, wdErr := os.Getwd()
		if wdErr != nil {
			return fmt.Errorf("failed to get working directory: %w", wdErr)
		}
		workDir = wd
	}

	// Create nebula branch if in a git repo.
	branchMgr, branchErr := nebula.NewBranchManager(ctx, workDir, n.Manifest.Nebula.Name)
	if branchErr != nil {
		fmt.Fprintf(os.Stderr, "warning: branch management unavailable: %v\n", branchErr)
	}
	if branchMgr != nil {
		if err := branchMgr.CreateOrCheckout(ctx); err != nil {
			return fmt.Errorf("failed to create nebula branch: %w", err)
		}
	}
	branchName := branchMgr.Branch()

	git := loop.NewCycleCommitterWithBranch(ctx, workDir, branchName)
	phaseCommitter := nebula.NewGitCommitterWithBranch(ctx, workDir, branchName)

	// Build TUI phase info.
	phases := make([]tui.PhaseInfo, 0, len(n.Phases))
	for _, p := range n.Phases {
		phases = append(phases, tui.PhaseInfo{
			ID:        p.ID,
			Title:     p.Title,
			DependsOn: p.DependsOn,
			PlanBody:  p.Body,
		})
	}

	tuiProgram := tui.NewNebulaProgram(n.Manifest.Nebula.Name, phases, dir, noSplash)

	wg := nebula.NewWorkerGroup(n, state,
		nebula.WithMaxWorkers(maxWorkers),
		nebula.WithBeadsClient(client),
		nebula.WithGlobalCycles(cfg.MaxReviewCycles),
		nebula.WithGlobalBudget(cfg.MaxBudgetUSD),
		nebula.WithGlobalModel(cfg.Model),
		nebula.WithCommitter(phaseCommitter),
	)

	wg.Runner = &tuiLoopAdapter{
		program:      tuiProgram,
		invoker:      claudeInv,
		beads:        client,
		git:          git,
		linter:       loop.NewLinter(cfg.LintCommands, workDir),
		maxCycles:    cfg.MaxReviewCycles,
		maxBudget:    cfg.MaxBudgetUSD,
		model:        cfg.Model,
		coderPrompt:  coderPrompt,
		reviewPrompt: reviewerPrompt,
		workDir:      workDir,
	}
	wg.Logger = io.Discard
	wg.Prompter = tui.NewGater(tuiProgram)
	wg.OnProgress = func(completed, total, openBeads, closedBeads int, totalCostUSD float64) {
		tuiProgram.Send(tui.MsgNebulaProgress{
			Completed:    completed,
			Total:        total,
			OpenBeads:    openBeads,
			ClosedBeads:  closedBeads,
			TotalCostUSD: totalCostUSD,
		})
	}
	wg.OnRefactor = func(phaseID string, pending bool) {
		if pending {
			tuiProgram.Send(tui.MsgPhaseRefactorPending{PhaseID: phaseID})
		}
	}

	// Create watcher for intervention file detection.
	w, watcherErr := nebula.NewWatcher(dir)
	if watcherErr != nil {
		fmt.Fprintf(os.Stderr, "warning: watcher unavailable: %v\n", watcherErr)
	} else {
		if startErr := w.Start(); startErr != nil {
			fmt.Fprintf(os.Stderr, "warning: watcher start failed: %v\n", startErr)
		} else {
			wg.Watcher = w
			defer w.Stop()
		}
	}

	// Run workers in goroutine; block on TUI.
	prog := tuiProgram
	br := branchName
	wd := workDir
	go func() {
		results, runErr := wg.Run(ctx)
		prog.Send(tui.MsgNebulaDone{Results: results, Err: runErr})
		if br != "" {
			gitResult := nebula.PostCompletion(context.Background(), wd, br)
			prog.Send(tui.MsgGitPostCompletion{Result: gitResult})
		}
	}()

	finalModel, tuiErr := tuiProgram.Run()
	cancel()
	if tuiErr != nil {
		return fmt.Errorf("TUI error: %w", tuiErr)
	}

	appModel, ok := finalModel.(tui.AppModel)
	if !ok {
		return nil
	}

	if appModel.DoneErr != nil && !errors.Is(appModel.DoneErr, nebula.ErrManualStop) {
		return appModel.DoneErr
	}

	return nil
}
