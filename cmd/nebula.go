package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

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

var nebulaCmd = &cobra.Command{
	Use:   "nebula",
	Short: "Manage nebula blueprints (validate, plan, apply, show, status)",
}

var nebulaValidateCmd = &cobra.Command{
	Use:   "validate <path>",
	Short: "Validate a nebula directory structure and dependencies",
	Args:  cobra.ExactArgs(1),
	RunE:  runNebulaValidate,
}

var nebulaPlanCmd = &cobra.Command{
	Use:   "plan <path>",
	Short: "Show what beads changes a nebula would produce",
	Args:  cobra.ExactArgs(1),
	RunE:  runNebulaPlan,
}

var nebulaApplyCmd = &cobra.Command{
	Use:   "apply <path>",
	Short: "Create/update beads from a nebula blueprint",
	Args:  cobra.ExactArgs(1),
	RunE:  runNebulaApply,
}

var nebulaShowCmd = &cobra.Command{
	Use:   "show <path>",
	Short: "Display current nebula state",
	Args:  cobra.ExactArgs(1),
	RunE:  runNebulaShow,
}

var nebulaStatusCmd = &cobra.Command{
	Use:   "status <path>",
	Short: "Display metrics summary for a nebula run",
	Args:  cobra.ExactArgs(1),
	RunE:  runNebulaStatus,
}

func init() {
	nebulaApplyCmd.Flags().Bool("auto", false, "automatically start workers for ready phases")
	nebulaApplyCmd.Flags().Bool("watch", false, "watch for phase file changes during execution (with --auto)")
	nebulaApplyCmd.Flags().Int("max-workers", 1, "maximum concurrent workers (with --auto)")
	nebulaApplyCmd.Flags().Bool("no-tui", false, "disable TUI even on a TTY (use stderr output)")

	nebulaStatusCmd.Flags().Bool("json", false, "output metrics as JSON to stdout")

	nebulaCmd.AddCommand(nebulaValidateCmd)
	nebulaCmd.AddCommand(nebulaPlanCmd)
	nebulaCmd.AddCommand(nebulaApplyCmd)
	nebulaCmd.AddCommand(nebulaShowCmd)
	nebulaCmd.AddCommand(nebulaStatusCmd)
	rootCmd.AddCommand(nebulaCmd)
}

func runNebulaValidate(cmd *cobra.Command, args []string) error {
	printer := ui.New()
	dir := args[0]

	n, err := nebula.Load(dir)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	errs := nebula.Validate(n)
	if len(errs) > 0 {
		printer.NebulaValidateResult(n.Manifest.Nebula.Name, len(n.Phases), errs)
		return fmt.Errorf("validation failed with %d error(s)", len(errs))
	}

	printer.NebulaValidateResult(n.Manifest.Nebula.Name, len(n.Phases), nil)
	return nil
}

func runNebulaPlan(cmd *cobra.Command, args []string) error {
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

	state, err := nebula.LoadState(dir)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	client := &beads.CLI{BeadsPath: cfg.BeadsPath, Verbose: cfg.Verbose}

	ctx := context.Background()
	plan, err := nebula.BuildPlan(ctx, n, state, client)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	printer.NebulaPlan(plan)
	return nil
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

	state, err := nebula.LoadState(dir)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	client := &beads.CLI{BeadsPath: cfg.BeadsPath, Verbose: cfg.Verbose}

	if v, _ := cmd.Flags().GetBool("verbose"); v {
		cfg.Verbose = true
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	workDir := cfg.WorkDir
	// Nebula context.working_dir overrides global if set.
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

	git := loop.NewCycleCommitter(ctx, workDir)

	noTUI, _ := cmd.Flags().GetBool("no-tui")
	useTUI := !noTUI && isStderrTTY()

	// Build the runner and WorkerGroup, branching on TUI vs stderr.
	var tuiProgram *tui.Program
	wg := nebula.NewWorkerGroup(n, state,
		nebula.WithMaxWorkers(maxWorkers),
		nebula.WithBeadsClient(client),
		nebula.WithGlobalCycles(cfg.MaxReviewCycles),
		nebula.WithGlobalBudget(cfg.MaxBudgetUSD),
		nebula.WithGlobalModel(cfg.Model),
	)

	if useTUI {
		// Build phase info and pre-populate the model (no Send before Run).
		phases := make([]tui.PhaseInfo, 0, len(n.Phases))
		for _, p := range n.Phases {
			phases = append(phases, tui.PhaseInfo{
				ID:        p.ID,
				Title:     p.Title,
				DependsOn: p.DependsOn,
				PlanBody:  p.Body,
			})
		}
		architectFunc := buildArchitectFunc(claudeInv, wg.SnapshotNebula)
		tuiProgram = tui.NewNebulaProgram(n.Manifest.Nebula.Name, phases, dir, architectFunc)
		// Per-phase loops with PhaseUIBridge for hierarchical TUI tracking.
		wg.Runner = &tuiLoopAdapter{
			program:      tuiProgram,
			invoker:      claudeInv,
			beads:        client,
			git:          git,
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
	} else {
		// Stderr path: single shared loop with Printer UI.
		taskLoop := &loop.Loop{
			Invoker:      claudeInv,
			Beads:        client,
			UI:           printer,
			Git:          git,
			Hooks:        []loop.Hook{&loop.BeadHook{Beads: client, UI: printer}},
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
			go func() {
				results, runErr := wg.Run(ctx)
				prog.Send(tui.MsgNebulaDone{Results: results, Err: runErr})
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

				phases := make([]tui.PhaseInfo, 0, len(nextN.Phases))
				for _, p := range nextN.Phases {
					phases = append(phases, tui.PhaseInfo{
						ID:        p.ID,
						Title:     p.Title,
						DependsOn: p.DependsOn,
						PlanBody:  p.Body,
					})
				}
				// Create WorkerGroup first so its SnapshotNebula method can be
				// captured by the architect closure. The Runner is set after the
				// TUI program is created (it depends on the program).
				wg = nebula.NewWorkerGroup(nextN, nextState,
					nebula.WithMaxWorkers(maxWorkers),
					nebula.WithBeadsClient(client),
					nebula.WithGlobalCycles(cfg.MaxReviewCycles),
					nebula.WithGlobalBudget(cfg.MaxBudgetUSD),
					nebula.WithGlobalModel(cfg.Model),
					nebula.WithLogger(io.Discard),
				)
				nextArchitectFunc := buildArchitectFunc(claudeInv, wg.SnapshotNebula)
				tuiProgram = tui.NewNebulaProgram(nextN.Manifest.Nebula.Name, phases, nextDir, nextArchitectFunc)
				wg.Runner = &tuiLoopAdapter{
					program:      tuiProgram,
					invoker:      claudeInv,
					beads:        client,
					git:          loop.NewCycleCommitter(ctx, nextWorkDir),
					maxCycles:    cfg.MaxReviewCycles,
					maxBudget:    cfg.MaxBudgetUSD,
					model:        cfg.Model,
					coderPrompt:  coderPrompt,
					reviewPrompt: reviewerPrompt,
					workDir:      nextWorkDir,
				}
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

				n = nextN
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
	return nil
}

// buildArchitectFunc creates a closure that invokes the nebula architect via the given invoker.
func buildArchitectFunc(invoker agent.Invoker, snapshotFn func() *nebula.Nebula) func(ctx context.Context, msg tui.MsgArchitectStart) (*nebula.ArchitectResult, error) {
	return func(ctx context.Context, msg tui.MsgArchitectStart) (*nebula.ArchitectResult, error) {
		return nebula.RunArchitect(ctx, invoker, nebula.ArchitectRequest{
			Mode:       nebula.ArchitectMode(msg.Mode),
			UserPrompt: msg.Prompt,
			Nebula:     snapshotFn(),
			PhaseID:    msg.PhaseID,
		})
	}
}

// loopAdapter wraps *loop.Loop to satisfy nebula.PhaseRunner.
type loopAdapter struct {
	loop *loop.Loop
}

func (a *loopAdapter) RunExistingPhase(ctx context.Context, phaseID, beadID, phaseDescription string, exec nebula.ResolvedExecution) (*nebula.PhaseRunnerResult, error) {
	// Apply per-phase execution overrides to the loop.
	if exec.MaxReviewCycles > 0 {
		a.loop.MaxCycles = exec.MaxReviewCycles
	}
	if exec.MaxBudgetUSD > 0 {
		a.loop.MaxBudgetUSD = exec.MaxBudgetUSD
	}
	if exec.Model != "" {
		a.loop.Model = exec.Model
	}

	result, err := a.loop.RunExistingTask(ctx, beadID, phaseDescription)
	if err != nil {
		return nil, err
	}
	return toPhaseRunnerResult(result), nil
}

func (a *loopAdapter) GenerateCheckpoint(ctx context.Context, beadID, phaseDescription string) (string, error) {
	return a.loop.GenerateCheckpoint(ctx, beadID, phaseDescription)
}

// tuiLoopAdapter creates a fresh loop per phase with a phase-specific PhaseUIBridge.
// This ensures each nebula phase sends UI messages tagged with its phase ID,
// enabling the TUI to track per-phase cycle timelines independently.
type tuiLoopAdapter struct {
	program      *tui.Program
	invoker      agent.Invoker
	beads        beads.Client
	git          loop.CycleCommitter
	maxCycles    int
	maxBudget    float64
	model        string
	coderPrompt  string
	reviewPrompt string
	workDir      string
}

func (a *tuiLoopAdapter) RunExistingPhase(ctx context.Context, phaseID, beadID, phaseDescription string, exec nebula.ResolvedExecution) (*nebula.PhaseRunnerResult, error) {
	// Create a per-phase UI bridge so messages carry the phase ID.
	phaseUI := tui.NewPhaseUIBridge(a.program, phaseID, a.workDir)

	l := &loop.Loop{
		Invoker:      a.invoker,
		Beads:        a.beads,
		UI:           phaseUI,
		Git:          a.git,
		Hooks:        []loop.Hook{&loop.BeadHook{Beads: a.beads, UI: phaseUI}},
		MaxCycles:    a.maxCycles,
		MaxBudgetUSD: a.maxBudget,
		Model:        a.model,
		CoderPrompt:  a.coderPrompt,
		ReviewPrompt: a.reviewPrompt,
		WorkDir:      a.workDir,
	}

	// Apply per-phase execution overrides.
	if exec.MaxReviewCycles > 0 {
		l.MaxCycles = exec.MaxReviewCycles
	}
	if exec.MaxBudgetUSD > 0 {
		l.MaxBudgetUSD = exec.MaxBudgetUSD
	}
	if exec.Model != "" {
		l.Model = exec.Model
	}

	result, err := l.RunExistingTask(ctx, beadID, phaseDescription)
	if err != nil {
		return nil, err
	}
	return toPhaseRunnerResult(result), nil
}

func (a *tuiLoopAdapter) GenerateCheckpoint(ctx context.Context, beadID, phaseDescription string) (string, error) {
	phaseUI := tui.NewPhaseUIBridge(a.program, "checkpoint", a.workDir)
	l := &loop.Loop{
		Invoker:      a.invoker,
		Beads:        a.beads,
		UI:           phaseUI,
		Git:          a.git,
		Hooks:        []loop.Hook{&loop.BeadHook{Beads: a.beads, UI: phaseUI}},
		MaxCycles:    a.maxCycles,
		MaxBudgetUSD: a.maxBudget,
		Model:        a.model,
		CoderPrompt:  a.coderPrompt,
		ReviewPrompt: a.reviewPrompt,
		WorkDir:      a.workDir,
	}
	return l.GenerateCheckpoint(ctx, beadID, phaseDescription)
}

// toPhaseRunnerResult converts a loop.TaskResult to nebula.PhaseRunnerResult.
func toPhaseRunnerResult(result *loop.TaskResult) *nebula.PhaseRunnerResult {
	return &nebula.PhaseRunnerResult{
		TotalCostUSD: result.TotalCostUSD,
		CyclesUsed:   result.CyclesUsed,
		Report:       result.Report,
	}
}

func runNebulaShow(cmd *cobra.Command, args []string) error {
	printer := ui.New()
	dir := args[0]

	n, err := nebula.Load(dir)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	state, err := nebula.LoadState(dir)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	printer.NebulaShow(n, state)
	return nil
}

func runNebulaStatus(cmd *cobra.Command, args []string) error {
	printer := ui.New()
	dir := args[0]

	n, err := nebula.Load(dir)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	state, err := nebula.LoadState(dir)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	metrics, history, err := nebula.LoadMetricsWithHistory(dir)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	jsonFlag, _ := cmd.Flags().GetBool("json")
	if jsonFlag {
		return writeStatusJSON(os.Stdout, n, state, metrics, history)
	}

	printer.NebulaStatus(n, state, metrics, history)
	return nil
}

// statusJSON is the structured representation of nebula status for --json output.
type statusJSON struct {
	Name        string            `json:"name"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
	TotalCost   float64           `json:"total_cost_usd"`
	TotalPhases int               `json:"total_phases"`
	Completed   int               `json:"completed"`
	Failed      int               `json:"failed"`
	Restarts    int               `json:"restarts"`
	Conflicts   int               `json:"conflicts"`
	DurationMs  int64             `json:"duration_ms,omitempty"`
	Waves       []statusWaveJSON  `json:"waves,omitempty"`
	Phases      []statusPhaseJSON `json:"phases,omitempty"`
	History     []statusRunJSON   `json:"history,omitempty"`
}

type statusWaveJSON struct {
	WaveNumber           int   `json:"wave_number"`
	PhaseCount           int   `json:"phase_count"`
	EffectiveParallelism int   `json:"effective_parallelism"`
	DurationMs           int64 `json:"duration_ms"`
	Conflicts            int   `json:"conflicts"`
}

type statusPhaseJSON struct {
	PhaseID      string  `json:"phase_id"`
	WaveNumber   int     `json:"wave_number"`
	DurationMs   int64   `json:"duration_ms"`
	CostUSD      float64 `json:"cost_usd"`
	CyclesUsed   int     `json:"cycles_used"`
	Restarts     int     `json:"restarts"`
	Satisfaction string  `json:"satisfaction,omitempty"`
	Conflict     bool    `json:"conflict"`
}

type statusRunJSON struct {
	StartedAt   time.Time `json:"started_at"`
	TotalPhases int       `json:"total_phases"`
	TotalCost   float64   `json:"total_cost_usd"`
	DurationMs  int64     `json:"duration_ms"`
	Conflicts   int       `json:"conflicts"`
}

// writeStatusJSON encodes the nebula status as JSON to the given writer.
func writeStatusJSON(w io.Writer, n *nebula.Nebula, state *nebula.State, m *nebula.Metrics, history []nebula.HistorySummary) error {
	out := statusJSON{
		Name:        n.Manifest.Nebula.Name,
		TotalPhases: len(n.Phases),
	}

	// Phase counts from state.
	for _, ps := range state.Phases {
		switch ps.Status {
		case nebula.PhaseStatusDone:
			out.Completed++
		case nebula.PhaseStatusFailed:
			out.Failed++
		}
	}

	// Cost from state as fallback.
	out.TotalCost = state.TotalCostUSD

	if m != nil {
		if !m.StartedAt.IsZero() {
			out.StartedAt = &m.StartedAt
		}
		if !m.CompletedAt.IsZero() {
			out.CompletedAt = &m.CompletedAt
		}
		if m.TotalCostUSD > 0 {
			out.TotalCost = m.TotalCostUSD
		}
		out.Restarts = m.TotalRestarts
		out.Conflicts = m.TotalConflicts

		if !m.StartedAt.IsZero() && !m.CompletedAt.IsZero() {
			out.DurationMs = m.CompletedAt.Sub(m.StartedAt).Milliseconds()
		}

		out.Waves = make([]statusWaveJSON, len(m.Waves))
		for i, w := range m.Waves {
			out.Waves[i] = statusWaveJSON{
				WaveNumber:           w.WaveNumber,
				PhaseCount:           w.PhaseCount,
				EffectiveParallelism: w.EffectiveParallelism,
				DurationMs:           w.TotalDuration.Milliseconds(),
				Conflicts:            w.Conflicts,
			}
		}

		out.Phases = make([]statusPhaseJSON, len(m.Phases))
		for i, p := range m.Phases {
			out.Phases[i] = statusPhaseJSON{
				PhaseID:      p.PhaseID,
				WaveNumber:   p.WaveNumber,
				DurationMs:   p.Duration.Milliseconds(),
				CostUSD:      p.CostUSD,
				CyclesUsed:   p.CyclesUsed,
				Restarts:     p.Restarts,
				Satisfaction: p.Satisfaction,
				Conflict:     p.Conflict,
			}
		}
	}

	out.History = make([]statusRunJSON, len(history))
	for i, h := range history {
		out.History[i] = statusRunJSON{
			StartedAt:   h.StartedAt,
			TotalPhases: h.TotalPhases,
			TotalCost:   h.TotalCostUSD,
			DurationMs:  h.Duration.Milliseconds(),
			Conflicts:   h.TotalConflicts,
		}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
