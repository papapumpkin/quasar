package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/beads"
	"github.com/papapumpkin/quasar/internal/claude"
	"github.com/papapumpkin/quasar/internal/config"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/loop"
	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/tui"
	"github.com/papapumpkin/quasar/internal/ui"
)

// addNebulaApplyFlags registers flags specific to the apply subcommand.
func addNebulaApplyFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("auto", false, "automatically start workers for ready phases")
	cmd.Flags().Bool("watch", false, "watch for phase file changes during execution (with --auto)")
	cmd.Flags().Int("max-workers", 1, "maximum concurrent workers (with --auto)")
	cmd.Flags().Bool("no-tui", false, "disable TUI even on a TTY (use stderr output)")
	cmd.Flags().Bool("no-splash", false, "skip the startup splash animation")
}

func runNebulaApply(cmd *cobra.Command, args []string) error {
	printer := ui.New()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	dir := args[0]

	n, err := nebula.Load(dir)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	if errs := nebula.Validate(n); len(errs) > 0 {
		printer.NebulaValidateResult(n.Manifest.Nebula.Name, len(n.Phases), errs)
		return fmt.Errorf("validation failed")
	}

	if v, _ := cmd.Flags().GetBool("verbose"); v {
		cfg.Verbose = true
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Resolve workDir and checkout nebula branch BEFORE loading state or
	// applying bead changes. The state file lives on the feature branch;
	// writing it before checkout creates an untracked file that blocks
	// the subsequent git checkout.
	workDir := cfg.WorkDir
	if n.Manifest.Context.WorkingDir != "" {
		workDir = n.Manifest.Context.WorkingDir
	}
	if workDir == "." || workDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
		workDir = wd
	}

	// Create nebula branch if in a git repo. Non-fatal if git is unavailable.
	branchMgr, branchErr := nebula.NewBranchManager(ctx, workDir, n.Manifest.Nebula.Name)
	if branchErr != nil {
		fmt.Fprintf(os.Stderr, "warning: branch management unavailable: %v\n", branchErr)
	}
	if branchMgr != nil {
		if err := branchMgr.CreateOrCheckout(ctx); err != nil {
			return fmt.Errorf("failed to create nebula branch: %w", err)
		}
	}
	branchName := branchMgr.Branch() // "" if branchMgr is nil (nil-safe)

	state, err := nebula.LoadState(dir)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	client := &beads.CLI{BeadsPath: cfg.BeadsPath, Verbose: cfg.Verbose}

	plan, err := nebula.BuildPlan(ctx, n, state, client)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	printer.NebulaPlan(plan)

	if !plan.HasChanges() {
		printer.Info("nothing to do")
		return nil
	}

	printer.Info("applying changes...")
	if err := nebula.Apply(ctx, plan, n, state, client); err != nil {
		printer.Error(err.Error())
		return err
	}
	printer.NebulaApplyDone(plan)

	// --auto: start workers.
	auto, _ := cmd.Flags().GetBool("auto")
	if !auto {
		return nil
	}

	maxWorkers, _ := cmd.Flags().GetInt("max-workers")
	maxWorkersChanged := cmd.Flags().Changed("max-workers")

	// If --max-workers was not explicitly set, use nebula execution config if available.
	if !maxWorkersChanged && n.Manifest.Execution.MaxWorkers > 0 {
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
		printer.Error(fmt.Sprintf("claude not available: %v", err))
		return err
	}

	// Initialize fabric infrastructure when the DAG has inter-phase dependencies.
	fc, err := initFabric(ctx, n, dir, workDir, claudeInv)
	if err != nil {
		return fmt.Errorf("fabric initialization failed: %w", err)
	}
	defer func() { fc.Close() }()

	git := loop.NewCycleCommitterWithBranch(ctx, workDir, branchName)
	phaseCommitter := nebula.NewGitCommitterWithBranch(ctx, workDir, branchName)

	noTUI, _ := cmd.Flags().GetBool("no-tui")
	noSplash, _ := cmd.Flags().GetBool("no-splash")
	useTUI := !noTUI && isStderrTTY()

	// Build the runner and WorkerGroup, branching on TUI vs stderr.
	var tuiProgram *tui.Program
	wgOpts := []nebula.Option{
		nebula.WithMaxWorkers(maxWorkers),
		nebula.WithBeadsClient(client),
		nebula.WithGlobalCycles(cfg.MaxReviewCycles),
		nebula.WithGlobalBudget(cfg.MaxBudgetUSD),
		nebula.WithGlobalModel(cfg.Model),
		nebula.WithCommitter(phaseCommitter),
	}
	wgOpts = append(wgOpts, fc.WorkerGroupOptions()...)
	wg := nebula.NewWorkerGroup(n, state, wgOpts...)

	if useTUI {
		// Build phase info and pre-populate the model (no Send before Run).
		phases := make([]tui.PhaseInfo, 0, len(n.Phases))
		for _, p := range n.Phases {
			pi := tui.PhaseInfo{
				ID:        p.ID,
				Title:     p.Title,
				DependsOn: p.DependsOn,
				PlanBody:  p.Body,
			}
			if ps := state.Phases[p.ID]; ps != nil {
				pi.Status = tui.PhaseStatusFromString(string(ps.Status))
			}
			phases = append(phases, pi)
		}
		tuiProgram = tui.NewNebulaProgram(n.Manifest.Nebula.Name, phases, dir, noSplash)
		// Per-phase loops with PhaseUIBridge for hierarchical TUI tracking.
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
			fabric:       wg.Fabric, // nil-safe — emitFabricEvents checks for nil
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
		// Wire Tycho OnHail callback to emit MsgHail via the TUI program.
		wg.OnHail = func(phaseID string, d fabric.Discovery) {
			tuiProgram.Send(tui.MsgHail{PhaseID: phaseID, Discovery: d})
		}
		// Wire fabric scanning notification to surface a toast when phases
		// enter the scanning gate (blocked → scanning → running).
		wg.OnScanning = func(phaseID string) {
			tuiProgram.Send(tui.MsgPhaseScanning{PhaseID: phaseID})
		}
		// Start telemetry bridge if a telemetry file exists.
		telemetryPath := filepath.Join(".quasar", "telemetry", "current.jsonl")
		if _, statErr := os.Stat(telemetryPath); statErr == nil {
			tb := tui.NewTelemetryBridge(tuiProgram, telemetryPath)
			if startErr := tb.Start(); startErr == nil {
				defer tb.Stop()
			}
		}
	} else {
		// Stderr path: single shared loop with Printer UI.
		taskLoop := &loop.Loop{
			Invoker:      claudeInv,
			UI:           printer,
			Git:          git,
			Hooks:        []loop.Hook{&loop.BeadHook{Beads: client, UI: printer}},
			Linter:       loop.NewLinter(cfg.LintCommands, workDir),
			MaxCycles:    cfg.MaxReviewCycles,
			MaxBudgetUSD: cfg.MaxBudgetUSD,
			Model:        cfg.Model,
			CoderPrompt:  coderPrompt,
			ReviewPrompt: reviewerPrompt,
			WorkDir:      workDir,
		}
		wg.Runner = &loopAdapter{loop: taskLoop}
		// Stderr path: use dashboard and terminal gater.
		isTTY := isStderrTTY()
		dashboard := nebula.NewDashboard(os.Stderr, n, state, cfg.MaxBudgetUSD, isTTY)
		if n.Manifest.Execution.Gate == nebula.GateModeWatch {
			dashboard.AppendOnly = true
		}
		wg.Dashboard = dashboard
		wg.OnProgress = dashboard.ProgressCallback()
	}

	// Always create a watcher for intervention file detection (PAUSE/STOP).
	w, err := nebula.NewWatcher(dir)
	if err != nil {
		printer.Error(fmt.Sprintf("failed to create watcher: %v", err))
	} else {
		if err := w.Start(); err != nil {
			printer.Error(fmt.Sprintf("failed to start watcher: %v", err))
		} else {
			wg.Watcher = w
			defer w.Stop()
		}
	}

	watch, _ := cmd.Flags().GetBool("watch")
	if watch {
		printer.Info("watching for phase file changes...")
	}

	if useTUI {
		for {
			// Run workers in a goroutine; block on TUI.
			// Capture tuiProgram in a local variable so the goroutine
			// always sends to the correct program instance, even if
			// tuiProgram is reassigned for a subsequent nebula.
			prog := tuiProgram
			br := branchName
			wd := workDir
			go func() {
				results, runErr := wg.Run(ctx)
				prog.Send(tui.MsgNebulaDone{Results: results, Err: runErr})
				// Post-completion git workflow: push branch and checkout main.
				if br != "" {
					gitResult := nebula.PostCompletion(context.Background(), wd, br)
					prog.Send(tui.MsgGitPostCompletion{Result: gitResult})
				}
			}()

			finalModel, tuiErr := tuiProgram.Run()
			// TUI exited — cancel context to stop any running workers.
			cancel()
			if tuiErr != nil {
				return fmt.Errorf("TUI error: %w", tuiErr)
			}

			appModel, ok := finalModel.(tui.AppModel)
			if !ok {
				return nil
			}

			// If the user selected a next nebula, re-launch with it.
			if appModel.NextNebula != "" {
				nextDir := appModel.NextNebula

				nextN, loadErr := nebula.Load(nextDir)
				if loadErr != nil {
					printer.Error(fmt.Sprintf("failed to load nebula: %v", loadErr))
					return loadErr
				}
				nextState, loadErr := nebula.LoadState(nextDir)
				if loadErr != nil {
					printer.Error(fmt.Sprintf("failed to load state: %v", loadErr))
					return loadErr
				}

				// Rebuild context and worker group for the new nebula.
				// cancel() was already called above after the TUI exited;
				// create a fresh context for the next iteration.
				ctx, cancel = context.WithCancel(context.Background())

				nextPlan, planErr := nebula.BuildPlan(ctx, nextN, nextState, client)
				if planErr != nil {
					cancel()
					printer.Error(fmt.Sprintf("failed to build plan: %v", planErr))
					return planErr
				}
				if nextPlan.HasChanges() {
					if applyErr := nebula.Apply(ctx, nextPlan, nextN, nextState, client); applyErr != nil {
						cancel()
						printer.Error(fmt.Sprintf("failed to apply: %v", applyErr))
						return applyErr
					}
				}

				// Determine work dir for next nebula.
				nextWorkDir := workDir
				if nextN.Manifest.Context.WorkingDir != "" {
					nextWorkDir = nextN.Manifest.Context.WorkingDir
				}

				// Create/checkout branch for the next nebula.
				nextBranchMgr, nextBranchErr := nebula.NewBranchManager(ctx, nextWorkDir, nextN.Manifest.Nebula.Name)
				if nextBranchErr != nil {
					fmt.Fprintf(os.Stderr, "warning: branch management unavailable: %v\n", nextBranchErr)
				}
				if nextBranchMgr != nil {
					if brErr := nextBranchMgr.CreateOrCheckout(ctx); brErr != nil {
						cancel()
						return fmt.Errorf("failed to create nebula branch: %w", brErr)
					}
				}
				nextBranchName := nextBranchMgr.Branch()

				// Close previous fabric before creating a new one.
				fc.Close()
				nextFC, nextFCErr := initFabric(ctx, nextN, nextDir, nextWorkDir, claudeInv)
				if nextFCErr != nil {
					cancel()
					return fmt.Errorf("fabric initialization failed: %w", nextFCErr)
				}
				fc = nextFC // reassign so deferred Close covers the new instance

				phases := make([]tui.PhaseInfo, 0, len(nextN.Phases))
				for _, p := range nextN.Phases {
					pi := tui.PhaseInfo{
						ID:        p.ID,
						Title:     p.Title,
						DependsOn: p.DependsOn,
						PlanBody:  p.Body,
					}
					if ps := nextState.Phases[p.ID]; ps != nil {
						pi.Status = tui.PhaseStatusFromString(string(ps.Status))
					}
					phases = append(phases, pi)
				}
				// Create WorkerGroup first. The Runner is set after the
				// TUI program is created (it depends on the program).
				nextPhaseCommitter := nebula.NewGitCommitterWithBranch(ctx, nextWorkDir, nextBranchName)
				nextWgOpts := []nebula.Option{
					nebula.WithMaxWorkers(maxWorkers),
					nebula.WithBeadsClient(client),
					nebula.WithGlobalCycles(cfg.MaxReviewCycles),
					nebula.WithGlobalBudget(cfg.MaxBudgetUSD),
					nebula.WithGlobalModel(cfg.Model),
					nebula.WithLogger(io.Discard),
					nebula.WithCommitter(nextPhaseCommitter),
				}
				nextWgOpts = append(nextWgOpts, fc.WorkerGroupOptions()...)
				wg = nebula.NewWorkerGroup(nextN, nextState, nextWgOpts...)
				tuiProgram = tui.NewNebulaProgram(nextN.Manifest.Nebula.Name, phases, nextDir, noSplash)
				wg.Runner = &tuiLoopAdapter{
					program:      tuiProgram,
					invoker:      claudeInv,
					beads:        client,
					git:          loop.NewCycleCommitterWithBranch(ctx, nextWorkDir, nextBranchName),
					linter:       loop.NewLinter(cfg.LintCommands, nextWorkDir),
					maxCycles:    cfg.MaxReviewCycles,
					maxBudget:    cfg.MaxBudgetUSD,
					model:        cfg.Model,
					coderPrompt:  coderPrompt,
					reviewPrompt: reviewerPrompt,
					workDir:      nextWorkDir,
					fabric:       wg.Fabric, // nil-safe
				}
				wg.Prompter = tui.NewGater(tuiProgram)
				// Re-wire OnHail for the next nebula's TUI program.
				wg.OnHail = func(phaseID string, d fabric.Discovery) {
					tuiProgram.Send(tui.MsgHail{PhaseID: phaseID, Discovery: d})
				}
				wg.OnScanning = func(phaseID string) {
					tuiProgram.Send(tui.MsgPhaseScanning{PhaseID: phaseID})
				}
				wg.OnProgress = func(completed, total, openBeads, closedBeads int, totalCostUSD float64) {
					tuiProgram.Send(tui.MsgNebulaProgress{
						Completed:    completed,
						Total:        total,
						OpenBeads:    openBeads,
						ClosedBeads:  closedBeads,
						TotalCostUSD: totalCostUSD,
					})
				}

				// Create a new watcher for the next nebula.
				if w != nil {
					w.Stop()
				}
				newW, watchErr := nebula.NewWatcher(nextDir)
				if watchErr == nil {
					if startErr := newW.Start(); startErr == nil {
						wg.Watcher = newW
						w = newW
					}
				}

				branchName = nextBranchName
				workDir = nextWorkDir
				continue
			}

			if appModel.DoneErr != nil {
				if !errors.Is(appModel.DoneErr, nebula.ErrManualStop) {
					printer.Error(appModel.DoneErr.Error())
				}
				return appModel.DoneErr
			}
			return nil
		}
	}

	// Stderr path: install signal handler for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		printer.Info("\nshutting down...")
		cancel()
	}()
	printer.Info(fmt.Sprintf("starting workers (max %d)...", maxWorkers))
	results, err := wg.Run(ctx)
	printer.NebulaProgressBarDone()
	if errors.Is(err, nebula.ErrManualStop) {
		printer.NebulaWorkerResults(results)
		return nil
	}
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	printer.NebulaWorkerResults(results)

	// Post-completion git workflow for stderr path.
	if branchName != "" {
		gitResult := nebula.PostCompletion(context.Background(), workDir, branchName)
		if gitResult.CommitErr != nil {
			printer.Error(fmt.Sprintf("git commit failed: %v", gitResult.CommitErr))
		}
		if gitResult.PushErr != nil {
			printer.Error(fmt.Sprintf("git push failed: %v", gitResult.PushErr))
		} else {
			printer.Info(fmt.Sprintf("pushed to origin/%s", gitResult.PushBranch))
		}
		if gitResult.CheckoutErr != nil {
			printer.Error(fmt.Sprintf("git checkout %s failed: %v", gitResult.CheckoutBranch, gitResult.CheckoutErr))
		} else {
			printer.Info(fmt.Sprintf("checked out %s", gitResult.CheckoutBranch))
		}
	}

	return nil
}
