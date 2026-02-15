package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/aaronsalm/quasar/internal/agent"
	"github.com/aaronsalm/quasar/internal/beads"
	"github.com/aaronsalm/quasar/internal/claude"
	"github.com/aaronsalm/quasar/internal/config"
	"github.com/aaronsalm/quasar/internal/loop"
	"github.com/aaronsalm/quasar/internal/nebula"
	"github.com/aaronsalm/quasar/internal/ui"
	"github.com/spf13/cobra"
)

var nebulaCmd = &cobra.Command{
	Use:   "nebula",
	Short: "Manage nebula blueprints (validate, plan, apply, show)",
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

func init() {
	nebulaApplyCmd.Flags().Bool("auto", false, "automatically start workers for ready tasks")
	nebulaApplyCmd.Flags().Bool("watch", false, "watch for task file changes during execution (with --auto)")
	nebulaApplyCmd.Flags().Int("max-workers", 1, "maximum concurrent workers (with --auto)")

	nebulaCmd.AddCommand(nebulaValidateCmd)
	nebulaCmd.AddCommand(nebulaPlanCmd)
	nebulaCmd.AddCommand(nebulaApplyCmd)
	nebulaCmd.AddCommand(nebulaShowCmd)
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
		printer.NebulaValidateResult(n.Manifest.Nebula.Name, len(n.Tasks), errs)
		return fmt.Errorf("validation failed with %d error(s)", len(errs))
	}

	printer.NebulaValidateResult(n.Manifest.Nebula.Name, len(n.Tasks), nil)
	return nil
}

func runNebulaPlan(cmd *cobra.Command, args []string) error {
	printer := ui.New()
	cfg := config.Load()
	dir := args[0]

	n, err := nebula.Load(dir)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	if errs := nebula.Validate(n); len(errs) > 0 {
		printer.NebulaValidateResult(n.Manifest.Nebula.Name, len(n.Tasks), errs)
		return fmt.Errorf("validation failed")
	}

	state, err := nebula.LoadState(dir)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	client := &beads.Client{BeadsPath: cfg.BeadsPath, Verbose: cfg.Verbose}
	plan, err := nebula.BuildPlan(n, state, client)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	printer.NebulaPlan(plan)
	return nil
}

func runNebulaApply(cmd *cobra.Command, args []string) error {
	printer := ui.New()
	cfg := config.Load()
	dir := args[0]

	n, err := nebula.Load(dir)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	if errs := nebula.Validate(n); len(errs) > 0 {
		printer.NebulaValidateResult(n.Manifest.Nebula.Name, len(n.Tasks), errs)
		return fmt.Errorf("validation failed")
	}

	state, err := nebula.LoadState(dir)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	client := &beads.Client{BeadsPath: cfg.BeadsPath, Verbose: cfg.Verbose}

	if v, _ := cmd.Flags().GetBool("verbose"); v {
		cfg.Verbose = true
	}

	plan, err := nebula.BuildPlan(n, state, client)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	printer.NebulaPlan(plan)

	if !plan.HasChanges() {
		printer.Info("nothing to do")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		printer.Info("\nshutting down...")
		cancel()
	}()

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

	claudeInv := &claude.Invoker{ClaudePath: cfg.ClaudePath, Verbose: cfg.Verbose}
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

	taskLoop := &loop.Loop{
		Invoker:      claudeInv,
		Beads:        client,
		UI:           printer,
		MaxCycles:    cfg.MaxReviewCycles,
		MaxBudgetUSD: cfg.MaxBudgetUSD,
		Model:        cfg.Model,
		CoderPrompt:  coderPrompt,
		ReviewPrompt: reviewerPrompt,
		WorkDir:      workDir,
	}

	wg := &nebula.WorkerGroup{
		Runner:       &loopAdapter{loop: taskLoop},
		Nebula:       n,
		State:        state,
		MaxWorkers:   maxWorkers,
		GlobalCycles: cfg.MaxReviewCycles,
		GlobalBudget: cfg.MaxBudgetUSD,
		GlobalModel:  cfg.Model,
		OnProgress:   printer.NebulaProgressBar,
	}

	watch, _ := cmd.Flags().GetBool("watch")
	if watch {
		w, err := nebula.NewWatcher(dir)
		if err != nil {
			printer.Error(fmt.Sprintf("failed to create watcher: %v", err))
		} else {
			if err := w.Start(); err != nil {
				printer.Error(fmt.Sprintf("failed to start watcher: %v", err))
			} else {
				wg.Watcher = w
				defer w.Stop()
				printer.Info("watching for task file changes...")
			}
		}
	}

	printer.Info(fmt.Sprintf("starting workers (max %d)...", maxWorkers))
	results, err := wg.Run(ctx)
	printer.NebulaProgressBarDone()
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	printer.NebulaWorkerResults(results)
	return nil
}

// loopAdapter wraps *loop.Loop to satisfy nebula.TaskRunner.
type loopAdapter struct {
	loop *loop.Loop
}

func (a *loopAdapter) RunExistingTask(ctx context.Context, beadID, taskDescription string, exec nebula.ResolvedExecution) (*nebula.TaskRunnerResult, error) {
	// Apply per-task execution overrides to the loop.
	if exec.MaxReviewCycles > 0 {
		a.loop.MaxCycles = exec.MaxReviewCycles
	}
	if exec.MaxBudgetUSD > 0 {
		a.loop.MaxBudgetUSD = exec.MaxBudgetUSD
	}
	if exec.Model != "" {
		a.loop.Model = exec.Model
	}

	result, err := a.loop.RunExistingTask(ctx, beadID, taskDescription)
	if err != nil {
		return nil, err
	}
	tr := &nebula.TaskRunnerResult{
		TotalCostUSD: result.TotalCostUSD,
		CyclesUsed:   result.CyclesUsed,
	}
	if result.Report != nil {
		tr.Report = &nebula.ReviewReport{
			Satisfaction:     result.Report.Satisfaction,
			Risk:             result.Report.Risk,
			NeedsHumanReview: result.Report.NeedsHumanReview,
			Summary:          result.Report.Summary,
		}
	}
	return tr, nil
}

func (a *loopAdapter) GenerateCheckpoint(ctx context.Context, beadID, taskDescription string) (string, error) {
	return a.loop.GenerateCheckpoint(ctx, beadID, taskDescription)
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
