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
	"time"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/beads"
	"github.com/papapumpkin/quasar/internal/bus"
	"github.com/papapumpkin/quasar/internal/checkpoint"
	"github.com/papapumpkin/quasar/internal/claude"
	"github.com/papapumpkin/quasar/internal/config"
	"github.com/papapumpkin/quasar/internal/loop"
	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/snapshot"
	"github.com/papapumpkin/quasar/internal/tui"
	"github.com/papapumpkin/quasar/internal/ui"
	"github.com/papapumpkin/quasar/internal/web"
)

// addNebulaApplyFlags registers flags specific to the apply subcommand.
func addNebulaApplyFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("auto", false, "automatically start workers for ready phases")
	cmd.Flags().Bool("watch", false, "watch for phase file changes during execution (with --auto)")
	cmd.Flags().Int("max-workers", 1, "maximum concurrent workers (with --auto)")
	cmd.Flags().Bool("no-tui", false, "disable TUI even on a TTY (use stderr output)")
	cmd.Flags().Bool("no-splash", false, "skip the startup splash animation")
	cmd.Flags().Int("max-context-tokens", 0, "token budget for injected context (0 = use default 10000)")
	cmd.Flags().Bool("resume", false, "resume from checkpoints if available (with --auto)")
	cmd.Flags().Bool("web", false, "launch web dashboard alongside execution")
	cmd.Flags().Int("web-port", 0, "port for web dashboard (0 = auto-assign)")
}

// runNebulaApply is the thin CLI adapter for "quasar nebula apply". It
// resolves CLI flags into an EngineConfig, delegates lifecycle management
// to the Engine, and selects the display path (TUI or stderr).
func runNebulaApply(cmd *cobra.Command, args []string) error {
	ecfg, err := resolveEngineConfig(cmd, args)
	if err != nil {
		return err
	}

	printer := ui.New()
	invoker := claude.NewInvoker(ecfg.ClaudePath, ecfg.Verbose)
	client := &beads.CLI{BeadsPath: ecfg.BeadsPath, Verbose: ecfg.Verbose}
	engine := nebula.NewEngine(ecfg, nil, invoker, client)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load, validate, resolve workDir, create branch, load state.
	if err := engine.Load(ctx); err != nil {
		return handleLoadError(printer, err)
	}

	// Build plan.
	if err := engine.Plan(ctx); err != nil {
		printer.Error(err.Error())
		return err
	}

	// Display and apply plan.
	printer.NebulaPlan(engine.GetPlan())
	if !engine.GetPlan().HasChanges() {
		printer.Info("nothing to do")
		return nil
	}
	printer.Info("applying changes...")
	if err := engine.ApplyPlan(ctx); err != nil {
		printer.Error(err.Error())
		return err
	}
	printer.NebulaApplyDone(engine.GetPlan())

	if !ecfg.Auto {
		return nil
	}

	// Validate invoker before starting workers.
	if err := invoker.Validate(); err != nil {
		printer.Error(fmt.Sprintf("claude not available: %v", err))
		return err
	}

	// Report checkpoint status for resume mode.
	if ecfg.Resume {
		var cpCount int
		if cps, loadErr := checkpoint.LoadAll(ecfg.NebulaDir); loadErr == nil {
			cpCount = len(cps)
		}
		printer.Info(fmt.Sprintf("resume mode: found %d checkpoint(s)", cpCount))
	}

	// Scan project context and initialize fabric.
	projectCtx := scanProjectContext(ctx, engine.WorkDir())
	fc, err := initFabric(ctx, engine.GetNebula(), ecfg.NebulaDir, engine.WorkDir(), invoker)
	if err != nil {
		return fmt.Errorf("fabric initialization failed: %w", err)
	}
	defer fc.Close()
	engine.SetFabric(fc)

	useWeb, _ := cmd.Flags().GetBool("web")
	webPort, _ := cmd.Flags().GetInt("web-port")

	if useWeb {
		return runApplyWithWeb(ctx, cancel, engine, invoker, client, ecfg, printer, projectCtx, webPort)
	}
	if ecfg.UseTUI {
		return runApplyWithTUI(ctx, cancel, engine, invoker, client, ecfg, printer, projectCtx, fc)
	}
	return runApplyWithStderr(ctx, cancel, engine, invoker, client, ecfg, printer, projectCtx)
}

// resolveEngineConfig reads CLI flags, Viper config, and derives the
// fully-resolved EngineConfig. This is the only function that touches
// Cobra/Viper — everything downstream works with EngineConfig.
func resolveEngineConfig(cmd *cobra.Command, args []string) (nebula.EngineConfig, error) {
	cfg, err := config.Load()
	if err != nil {
		return nebula.EngineConfig{}, fmt.Errorf("load config: %w", err)
	}
	if v, _ := cmd.Flags().GetBool("verbose"); v {
		cfg.Verbose = true
	}

	auto, _ := cmd.Flags().GetBool("auto")
	resume, _ := cmd.Flags().GetBool("resume")
	if resume && !auto {
		fmt.Fprintf(os.Stderr, "warning: --resume requires --auto; ignoring\n")
		resume = false
	}

	noTUI, _ := cmd.Flags().GetBool("no-tui")
	noSplash, _ := cmd.Flags().GetBool("no-splash")
	watch, _ := cmd.Flags().GetBool("watch")
	maxWorkers, _ := cmd.Flags().GetInt("max-workers")
	maxContextTokens, _ := cmd.Flags().GetInt("max-context-tokens")

	coderPrompt, reviewerPrompt := resolvePrompts(cfg)

	return nebula.EngineConfig{
		NebulaDir:                args[0],
		WorkDir:                  cfg.WorkDir,
		MaxWorkers:               maxWorkers,
		MaxWorkersExplicit:       cmd.Flags().Changed("max-workers"),
		MaxReviewCycles:          cfg.MaxReviewCycles,
		MaxBudgetUSD:             cfg.MaxBudgetUSD,
		MaxContextTokens:         maxContextTokens,
		MaxContextTokensExplicit: cmd.Flags().Changed("max-context-tokens"),
		Model:                    cfg.Model,
		CoderPrompt:              coderPrompt,
		ReviewerPrompt:           reviewerPrompt,
		Verbose:                  cfg.Verbose,
		Auto:                     auto,
		Resume:                   resume,
		UseTUI:                   auto && !noTUI && isStderrTTY(),
		NoSplash:                 noSplash,
		Watch:                    watch,
		LintCommands:             cfg.LintCommands,
		ClaudePath:               cfg.ClaudePath,
		BeadsPath:                cfg.BeadsPath,
		CacheOptimization:        cfg.CacheOptimization,
		CacheVerbose:             cfg.CacheVerbose,
		FixEffort:                cfg.FixEffort,
		FallbackModel:            cfg.FallbackModel,
	}, nil
}

// resolvePrompts returns the coder and reviewer system prompts,
// preferring custom prompts from config when set.
func resolvePrompts(cfg config.Config) (coderPrompt, reviewerPrompt string) {
	coderPrompt = agent.DefaultCoderSystemPrompt
	if cfg.CoderSystemPrompt != "" {
		coderPrompt = cfg.CoderSystemPrompt
	}
	reviewerPrompt = agent.DefaultReviewerSystemPrompt
	if cfg.ReviewerSystemPrompt != "" {
		reviewerPrompt = cfg.ReviewerSystemPrompt
	}
	return coderPrompt, reviewerPrompt
}

// engineConfigFromSettings builds an EngineConfig from a config.Config
// and cockpit-specific parameters. This is the counterpart of
// resolveEngineConfig for the cockpit path, which doesn't have a
// cobra.Command.
func engineConfigFromSettings(cfg config.Config, dir string, noSplash bool, maxWorkers int, maxWorkersExplicit bool) nebula.EngineConfig {
	coderPrompt, reviewerPrompt := resolvePrompts(cfg)
	return nebula.EngineConfig{
		NebulaDir:          dir,
		WorkDir:            cfg.WorkDir,
		MaxWorkers:         maxWorkers,
		MaxWorkersExplicit: maxWorkersExplicit,
		MaxReviewCycles:    cfg.MaxReviewCycles,
		MaxBudgetUSD:       cfg.MaxBudgetUSD,
		Model:              cfg.Model,
		CoderPrompt:        coderPrompt,
		ReviewerPrompt:     reviewerPrompt,
		Verbose:            cfg.Verbose,
		Auto:               true,
		UseTUI:             true,
		NoSplash:           noSplash,
		LintCommands:       cfg.LintCommands,
		ClaudePath:         cfg.ClaudePath,
		BeadsPath:          cfg.BeadsPath,
		CacheOptimization:  cfg.CacheOptimization,
		CacheVerbose:       cfg.CacheVerbose,
		FixEffort:          cfg.FixEffort,
		FallbackModel:      cfg.FallbackModel,
	}
}

// runApplyWithTUI runs the nebula execution with the TUI display path.
// It supports the "next nebula" loop: when the user picks a different
// nebula from the completion overlay, a new Engine is created and re-run.
func runApplyWithTUI(
	ctx context.Context,
	cancel context.CancelFunc,
	engine *nebula.Engine,
	invoker *claude.Invoker,
	client beads.Client,
	ecfg nebula.EngineConfig,
	printer *ui.Printer,
	projectCtx string,
	fc *fabricComponents,
) error {
	for {
		appModel, err := executeTUIRun(ctx, cancel, engine, invoker, client, ecfg, projectCtx)
		if err != nil {
			return err
		}

		// Handle next-nebula selection.
		if appModel.NextNebula != "" {
			engine, ecfg, fc, ctx, cancel, err = prepareNextNebula(
				appModel.NextNebula, ecfg, invoker, client, fc,
			)
			if err != nil {
				printer.Error(err.Error())
				return err
			}
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

// executeTUIRun performs a single TUI execution cycle. It creates the TUI
// program, wires bus/telemetry infrastructure, runs the engine in a
// background goroutine, and blocks on the TUI. Returns the final AppModel
// for the caller to interpret.
func executeTUIRun(
	ctx context.Context,
	cancel context.CancelFunc,
	engine *nebula.Engine,
	invoker *claude.Invoker,
	client beads.Client,
	ecfg nebula.EngineConfig,
	projectCtx string,
) (tui.AppModel, error) {
	ecfg = engine.Config() // pick up manifest-resolved values

	phases := buildTUIPhaseInfos(engine.GetNebula(), engine.GetState())
	tuiProgram := tui.NewNebulaProgram(
		engine.GetNebula().Manifest.Nebula.Name, phases, ecfg.NebulaDir, ecfg.NoSplash,
	)

	// Wire bus infrastructure.
	eventBus := bus.NewMemoryBus()
	busSub := tui.NewBusSubscriber(tuiProgram, eventBus, 128)
	busSub.Start()

	// Telemetry bridge (non-fatal).
	telemetryPath := filepath.Join(".quasar", "telemetry", "current.jsonl")
	if _, statErr := os.Stat(telemetryPath); statErr == nil {
		tb := tui.NewTelemetryBridge(tuiProgram, telemetryPath)
		if startErr := tb.Start(); startErr == nil {
			defer tb.Stop()
		}
	}

	// Build worker options for the TUI path.
	wgOpts := buildTUIWorkerOpts(engine, invoker, client, ecfg, tuiProgram, eventBus, projectCtx)

	// Resume checkpoint handling.
	cpDir := ecfg.NebulaDir
	isResume := ecfg.Resume
	var cpCount int
	if isResume {
		if cps, loadErr := checkpoint.LoadAll(cpDir); loadErr == nil {
			cpCount = len(cps)
		}
	}

	prog := tuiProgram
	go func() {
		if isResume {
			prog.Send(tui.MsgInfo{Msg: fmt.Sprintf("resume mode: found %d checkpoint(s)", cpCount)})
		}
		results, runErr := engine.Execute(ctx, wgOpts...)
		if runErr == nil {
			cleanupCheckpoints(cpDir)
		}
		prog.Send(tui.MsgNebulaDone{Results: results, Err: runErr})
		if gitResult := engine.PostComplete(context.Background()); gitResult != nil {
			prog.Send(tui.MsgGitPostCompletion{Result: gitResult})
		}
	}()

	finalModel, tuiErr := tuiProgram.Run()
	cancel()
	busSub.Stop()
	eventBus.Close()

	if tuiErr != nil {
		return tui.AppModel{}, fmt.Errorf("TUI error: %w", tuiErr)
	}

	appModel, ok := finalModel.(tui.AppModel)
	if !ok {
		return tui.AppModel{}, nil
	}
	return appModel, nil
}

// buildStderrWorkerOpts constructs the loop, dashboard, and worker options
// for the stderr-based display paths (plain stderr and web). The optional
// eventBus is wired in when non-nil (used by the web path).
func buildStderrWorkerOpts(
	ctx context.Context,
	engine *nebula.Engine,
	invoker *claude.Invoker,
	client beads.Client,
	ecfg nebula.EngineConfig,
	printer *ui.Printer,
	projectCtx string,
	eventBus *bus.MemoryBus,
) []nebula.Option {
	n := engine.GetNebula()
	state := engine.GetState()
	workDir := engine.WorkDir()

	git := loop.NewCycleCommitterWithBranch(ctx, workDir, engine.BranchName())
	taskLoop := &loop.Loop{
		Invoker:           invoker,
		UI:                printer,
		Git:               git,
		Hooks:             []loop.Hook{&loop.BeadHook{Beads: client, UI: printer}},
		Linter:            loop.NewLinter(ecfg.LintCommands, workDir),
		MaxCycles:         ecfg.MaxReviewCycles,
		MaxBudgetUSD:      ecfg.MaxBudgetUSD,
		Model:             ecfg.Model,
		CoderPrompt:       ecfg.CoderPrompt,
		ReviewPrompt:      ecfg.ReviewerPrompt,
		WorkDir:           workDir,
		ProjectContext:    projectCtx,
		MaxContextTokens:  ecfg.MaxContextTokens,
		CacheOptimization: ecfg.CacheOptimization,
		CacheVerbose:      ecfg.CacheVerbose,
		CheckpointDir:     ecfg.NebulaDir,
		FixEffort:         ecfg.FixEffort,
		FallbackModel:     ecfg.FallbackModel,
	}

	isTTY := isStderrTTY()
	dashboard := nebula.NewDashboard(os.Stderr, n, state, ecfg.MaxBudgetUSD, isTTY)
	if n.Manifest.Execution.Gate == nebula.GateModeWatch {
		dashboard.AppendOnly = true
	}

	opts := []nebula.Option{
		nebula.WithRunner(&loopAdapter{loop: taskLoop}),
		nebula.WithDashboard(dashboard),
		nebula.WithOnProgress(dashboard.ProgressCallback()),
	}
	if eventBus != nil {
		opts = append(opts, nebula.WithBus(eventBus))
	}
	opts = append(opts, resumeOptions(ecfg.Resume, workDir)...)
	return opts
}

// installSignalHandler installs a SIGINT/SIGTERM handler that cancels the
// context and prints a shutdown message. Returns a cleanup function that
// stops signal delivery (call in defer).
func installSignalHandler(cancel context.CancelFunc, printer *ui.Printer) func() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		printer.Info("\nshutting down...")
		cancel()
	}()
	return func() { signal.Stop(sigCh) }
}

// handleExecutionResult processes the results of a nebula execution:
// displays worker results, cleans up checkpoints, and runs the
// post-completion git workflow. Returns the execution error if fatal.
func handleExecutionResult(
	results []nebula.WorkerResult,
	runErr error,
	engine *nebula.Engine,
	ecfg nebula.EngineConfig,
	printer *ui.Printer,
) error {
	if errors.Is(runErr, nebula.ErrManualStop) {
		printer.NebulaWorkerResults(results)
		return nil
	}
	if runErr != nil {
		printer.Error(runErr.Error())
		return runErr
	}

	printer.NebulaWorkerResults(results)
	cleanupCheckpoints(ecfg.NebulaDir)

	// Post-completion git workflow.
	if gitResult := engine.PostComplete(context.Background()); gitResult != nil {
		printGitResult(printer, gitResult)
	}

	return nil
}

// runApplyWithStderr runs the nebula execution with the stderr display path.
func runApplyWithStderr(
	ctx context.Context,
	cancel context.CancelFunc,
	engine *nebula.Engine,
	invoker *claude.Invoker,
	client beads.Client,
	ecfg nebula.EngineConfig,
	printer *ui.Printer,
	projectCtx string,
) error {
	wgOpts := buildStderrWorkerOpts(ctx, engine, invoker, client, ecfg, printer, projectCtx, nil)

	stopSignal := installSignalHandler(cancel, printer)
	defer stopSignal()

	printer.Info(fmt.Sprintf("starting workers (max %d)...", ecfg.MaxWorkers))
	results, err := engine.Execute(ctx, wgOpts...)
	printer.NebulaProgressBarDone()

	return handleExecutionResult(results, err, engine, ecfg, printer)
}

// runApplyWithWeb runs the nebula execution with a web dashboard instead of
// the TUI or stderr display. It starts an HTTP server with SSE event
// streaming, runs the engine, then shuts down gracefully.
func runApplyWithWeb(
	ctx context.Context,
	cancel context.CancelFunc,
	engine *nebula.Engine,
	invoker *claude.Invoker,
	client beads.Client,
	ecfg nebula.EngineConfig,
	printer *ui.Printer,
	projectCtx string,
	port int,
) error {
	// Create event bus for the web server.
	eventBus := bus.NewMemoryBus()
	defer eventBus.Close()

	// Create and start web server.
	srv, err := web.NewServer(web.ServerConfig{
		Source:    web.NewBusAdapter(eventBus),
		NebulaDir: ecfg.NebulaDir,
		Port:      port,
	})
	if err != nil {
		return fmt.Errorf("create web server: %w", err)
	}

	srvCtx, srvCancel := context.WithCancel(ctx)
	defer srvCancel()

	addr, err := srv.Start(srvCtx)
	if err != nil {
		return fmt.Errorf("start web server: %w", err)
	}
	printer.Info(fmt.Sprintf("[web] dashboard at http://%s", addr))
	openBrowser(fmt.Sprintf("http://%s", addr))

	// Wire nebula data into the web dashboard for rendering.
	srv.SetNebula(engine.GetNebula(), engine.GetState())

	wgOpts := buildStderrWorkerOpts(ctx, engine, invoker, client, ecfg, printer, projectCtx, eventBus)

	stopSignal := installSignalHandler(cancel, printer)
	defer stopSignal()

	printer.Info(fmt.Sprintf("starting workers (max %d)...", ecfg.MaxWorkers))
	results, runErr := engine.Execute(ctx, wgOpts...)
	printer.NebulaProgressBarDone()

	// Keep server alive briefly for final SSE delivery, then shut down.
	time.AfterFunc(2*time.Second, srvCancel)
	srv.Wait()

	return handleExecutionResult(results, runErr, engine, ecfg, printer)
}

// buildTUIWorkerOpts constructs the worker options for the TUI display path.
func buildTUIWorkerOpts(
	engine *nebula.Engine,
	invoker *claude.Invoker,
	client beads.Client,
	ecfg nebula.EngineConfig,
	tuiProgram *tui.Program,
	eventBus *bus.MemoryBus,
	projectCtx string,
) []nebula.Option {
	workDir := engine.WorkDir()
	git := loop.NewCycleCommitterWithBranch(context.Background(), workDir, engine.BranchName())
	runner := &tuiLoopAdapter{
		program:          tuiProgram,
		invoker:          invoker,
		beads:            client,
		git:              git,
		linter:           loop.NewLinter(ecfg.LintCommands, workDir),
		maxCycles:        ecfg.MaxReviewCycles,
		maxBudget:        ecfg.MaxBudgetUSD,
		model:            ecfg.Model,
		coderPrompt:      ecfg.CoderPrompt,
		reviewPrompt:     ecfg.ReviewerPrompt,
		workDir:          workDir,
		bus:              eventBus,
		projectContext:   projectCtx,
		maxContextTokens: ecfg.MaxContextTokens,
		checkpointDir:    ecfg.NebulaDir,
		fixEffort:        ecfg.FixEffort,
		fallbackModel:    ecfg.FallbackModel,
	}

	opts := []nebula.Option{
		nebula.WithRunner(runner),
		nebula.WithLogger(io.Discard),
		nebula.WithBus(eventBus),
		nebula.WithPrompter(tui.NewGater(tuiProgram)),
		nebula.WithDialog(tui.NewDialogBridge(tuiProgram)),
	}
	opts = append(opts, resumeOptions(ecfg.Resume, workDir)...)
	return opts
}

// prepareNextNebula creates a new Engine for the next nebula selected from
// the TUI completion overlay. It handles load, plan, apply, and fabric init.
func prepareNextNebula(
	nextDir string,
	ecfg nebula.EngineConfig,
	invoker *claude.Invoker,
	client beads.Client,
	prevFC *fabricComponents,
) (*nebula.Engine, nebula.EngineConfig, *fabricComponents, context.Context, context.CancelFunc, error) {
	nextEcfg := ecfg
	nextEcfg.NebulaDir = nextDir
	nextEcfg.NoSplash = true

	ctx, cancel := context.WithCancel(context.Background())

	engine := nebula.NewEngine(nextEcfg, nil, invoker, client)
	if err := engine.Load(ctx); err != nil {
		cancel()
		return nil, ecfg, prevFC, nil, nil, err
	}
	if err := engine.Plan(ctx); err != nil {
		cancel()
		return nil, ecfg, prevFC, nil, nil, err
	}
	if engine.GetPlan().HasChanges() {
		if err := engine.ApplyPlan(ctx); err != nil {
			cancel()
			return nil, ecfg, prevFC, nil, nil, err
		}
	}

	// Close previous fabric and create a new one.
	prevFC.Close()
	nextFC, fcErr := initFabric(ctx, engine.GetNebula(), nextDir, engine.WorkDir(), invoker)
	if fcErr != nil {
		cancel()
		return nil, ecfg, prevFC, nil, nil, fmt.Errorf("fabric initialization failed: %w", fcErr)
	}
	engine.SetFabric(nextFC)

	nextEcfg = engine.Config() // pick up manifest-resolved values
	return engine, nextEcfg, nextFC, ctx, cancel, nil
}

// buildTUIPhaseInfos converts nebula phases and state into TUI PhaseInfo
// entries for pre-populating the phase table.
func buildTUIPhaseInfos(n *nebula.Nebula, state *nebula.State) []tui.PhaseInfo {
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
	return phases
}

// handleLoadError displays a structured error for validation failures or
// a generic error message for other load errors.
func handleLoadError(printer *ui.Printer, err error) error {
	var valErr *nebula.ValidationFailedError
	if errors.As(err, &valErr) {
		printer.NebulaValidateResult(valErr.Name, valErr.PhaseCount, valErr.Errors)
	} else {
		printer.Error(err.Error())
	}
	return err
}

// scanProjectContext scans the working directory for project context to
// inject into prompts. Non-fatal on failure.
func scanProjectContext(ctx context.Context, workDir string) string {
	scanner := &snapshot.Scanner{WorkDir: workDir}
	scanned, err := scanner.Scan(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: project context scan failed: %v\n", err)
		return ""
	}
	return scanned
}

// printGitResult displays the post-completion git workflow result.
func printGitResult(printer *ui.Printer, result *nebula.PostCompletionResult) {
	if result.CommitErr != nil {
		printer.Error(fmt.Sprintf("git commit failed: %v", result.CommitErr))
	}
	if result.PushErr != nil {
		printer.Error(fmt.Sprintf("git push failed: %v", result.PushErr))
	} else {
		printer.Info(fmt.Sprintf("pushed to origin/%s", result.PushBranch))
	}
}

// resumeOptions returns nebula.Option values that enable checkpoint-based
// resume for a WorkerGroup. When resume is false it returns nil.
// workDir is used in the GitSHA closure so each nebula gets the correct
// working directory for HEAD resolution.
func resumeOptions(resume bool, workDir string) []nebula.Option {
	if !resume {
		return nil
	}
	return []nebula.Option{
		nebula.WithResumeEnabled(true),
		nebula.WithCheckpointLoader(func(cpDir, phaseID string) (any, error) {
			return checkpoint.Load(cpDir, phaseID)
		}),
		nebula.WithCheckpointValidator(func(cp any, gitSHA string) error {
			typed, ok := cp.(*checkpoint.Checkpoint)
			if !ok {
				return fmt.Errorf("invalid checkpoint type: expected *checkpoint.Checkpoint")
			}
			return checkpoint.Validate(typed, gitSHA)
		}),
		nebula.WithCheckpointRemover(checkpoint.Remove),
		nebula.WithGitSHAFunc(func(ctx context.Context) (string, error) {
			return checkpoint.CurrentGitSHA(ctx, workDir)
		}),
	}
}

// cleanupCheckpoints removes all checkpoint files in the given directory.
// This is called after a successful nebula run to prevent stale checkpoints
// from affecting future --resume invocations.
func cleanupCheckpoints(dir string) {
	cps, err := checkpoint.LoadAll(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to list checkpoints for cleanup: %v\n", err)
		return
	}
	for phaseID := range cps {
		if rmErr := checkpoint.Remove(dir, phaseID); rmErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to remove checkpoint for %s: %v\n", phaseID, rmErr)
		}
	}
}
