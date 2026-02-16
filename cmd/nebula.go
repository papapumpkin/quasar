package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/aaronsalm/quasar/internal/agent"
	"github.com/aaronsalm/quasar/internal/agentmail"
	"github.com/aaronsalm/quasar/internal/beads"
	"github.com/aaronsalm/quasar/internal/claude"
	"github.com/aaronsalm/quasar/internal/config"
	"github.com/aaronsalm/quasar/internal/loop"
	"github.com/aaronsalm/quasar/internal/nebula"
	"github.com/aaronsalm/quasar/internal/ui"
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
	nebulaApplyCmd.Flags().Bool("auto", false, "automatically start workers for ready phases")
	nebulaApplyCmd.Flags().Bool("watch", false, "watch for phase file changes during execution (with --auto)")
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
		printer.NebulaValidateResult(n.Manifest.Nebula.Name, len(n.Phases), errs)
		return fmt.Errorf("validation failed with %d error(s)", len(errs))
	}

	printer.NebulaValidateResult(n.Manifest.Nebula.Name, len(n.Phases), nil)
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
	cfg := config.Load()
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

	// Agentmail MCP server lifecycle.
	// Enabled via .quasar.yaml (agentmail.enabled) or nebula [execution] (agentmail = true).
	var mcpCfg *agent.MCPConfig
	amEnabled := cfg.AgentMail.Enabled || n.Manifest.Execution.AgentMail
	if amEnabled {
		amPort := cfg.AgentMail.Port
		if n.Manifest.Execution.AgentMailPort > 0 {
			amPort = n.Manifest.Execution.AgentMailPort
		}
		amDSN := cfg.AgentMail.DoltDSN

		amBinary, lookErr := exec.LookPath("agentmail")
		if lookErr != nil {
			printer.Error("agentmail binary not found in PATH; build with: go build -o agentmail ./cmd/agentmail")
			return fmt.Errorf("agentmail binary not found: %w", lookErr)
		}

		srv := &agentmail.ProcessManager{BinaryPath: amBinary, Port: amPort, DoltDSN: amDSN}
		printer.Info(fmt.Sprintf("starting agentmail server on port %d...", amPort))
		if startErr := srv.Start(ctx); startErr != nil {
			printer.Error(fmt.Sprintf("agentmail server failed to start: %v", startErr))
			return startErr
		}
		defer func() {
			printer.Info("stopping agentmail server...")
			if stopErr := srv.Stop(); stopErr != nil {
				printer.Error(fmt.Sprintf("agentmail stop: %v", stopErr))
			}
		}()

		tmpDir, tmpErr := os.MkdirTemp("", "quasar-mcp-*")
		if tmpErr != nil {
			return fmt.Errorf("failed to create temp dir for MCP config: %w", tmpErr)
		}
		defer os.RemoveAll(tmpDir)

		cfgPath, genErr := agentmail.GenerateMCPConfig(tmpDir, amPort)
		if genErr != nil {
			return genErr
		}
		mcpCfg = &agent.MCPConfig{ConfigPath: cfgPath}
		printer.Info(fmt.Sprintf("agentmail MCP config: %s", cfgPath))
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
		MCP:          mcpCfg,
	}

	// Detect TTY for dashboard rendering mode.
	isTTY := false
	if fi, err := os.Stderr.Stat(); err == nil {
		isTTY = (fi.Mode() & os.ModeCharDevice) != 0
	}
	dashboard := nebula.NewDashboard(os.Stderr, n, state, cfg.MaxBudgetUSD, isTTY)

	// Enable append-only dashboard when the global gate mode is watch.
	// This avoids cursor movement so checkpoint blocks remain visible in scroll-back.
	if n.Manifest.Execution.Gate == nebula.GateModeWatch {
		dashboard.AppendOnly = true
	}

	wg := &nebula.WorkerGroup{
		Runner:       &loopAdapter{loop: taskLoop},
		Nebula:       n,
		State:        state,
		MaxWorkers:   maxWorkers,
		Dashboard:    dashboard,
		GlobalCycles: cfg.MaxReviewCycles,
		GlobalBudget: cfg.MaxBudgetUSD,
		GlobalModel:  cfg.Model,
		OnProgress:   dashboard.ProgressCallback(),
	}

	// Always create a watcher for intervention file detection (PAUSE/STOP).
	// The --watch flag additionally enables phase file change monitoring.
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

	printer.Info(fmt.Sprintf("starting workers (max %d)...", maxWorkers))
	results, err := wg.Run(ctx)
	printer.NebulaProgressBarDone()
	if errors.Is(err, nebula.ErrManualStop) {
		// Manual stop is not a failure — return results but no error.
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

// loopAdapter wraps *loop.Loop to satisfy nebula.PhaseRunner.
type loopAdapter struct {
	loop *loop.Loop
}

func (a *loopAdapter) RunExistingPhase(ctx context.Context, beadID, phaseDescription string, exec nebula.ResolvedExecution) (*nebula.PhaseRunnerResult, error) {
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
	tr := &nebula.PhaseRunnerResult{
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

func (a *loopAdapter) GenerateCheckpoint(ctx context.Context, beadID, phaseDescription string) (string, error) {
	return a.loop.GenerateCheckpoint(ctx, beadID, phaseDescription)
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
