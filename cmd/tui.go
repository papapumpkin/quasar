package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/claude"
	"github.com/papapumpkin/quasar/internal/config"
	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/tui"
	"github.com/papapumpkin/quasar/internal/ui"
)

// cockpitCmd launches the TUI home screen for browsing and running nebulas.
var cockpitCmd = &cobra.Command{
	Use:   "cockpit",
	Short: "Launch the interactive cockpit home screen",
	Long: `Launch the Quasar cockpit home screen that auto-discovers nebulas in
the .nebulas/ directory of the current (or specified) directory. From the
landing page you can browse nebulas, see their status, and select one to
run.`,
	Args: cobra.NoArgs,
	RunE: runTUI,
}

func init() {
	cockpitCmd.Flags().String("dir", "", "directory to scan for .nebulas/ (default: cwd)")
	cockpitCmd.Flags().Bool("no-splash", false, "skip the startup splash animation")
	cockpitCmd.Flags().Int("max-workers", 1, "maximum concurrent workers")
	rootCmd.AddCommand(cockpitCmd)
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
		return fmt.Errorf("quasar cockpit requires a TTY (terminal)")
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
		result := runSelectedNebula(cfg, printer, selectedDir, noSplash, maxWorkers, maxWorkersExplicit)
		if result.Err != nil {
			printer.Error(fmt.Sprintf("nebula execution error: %v", result.Err))
			// Don't exit — return to the home screen.
		}

		// After splash is shown once, skip it on subsequent iterations.
		noSplash = true

		// Determine what to do after nebula completion.
		switch {
		case result.ReturnToHome:
			// User pressed Esc on completion overlay — loop back to re-discover.
			continue
		case result.NextNebula != "":
			// User selected a nebula from the picker — run it directly, then
			// loop back so the home screen refreshes afterward.
			nextResult := runSelectedNebula(cfg, printer, result.NextNebula, true, maxWorkers, maxWorkersExplicit)
			if nextResult.Err != nil {
				printer.Error(fmt.Sprintf("nebula execution error: %v", nextResult.Err))
			}
			// After running the next nebula, always return to home.
			continue
		default:
			// User pressed q or exited without selecting — quit entirely.
			return result.Err
		}
	}
}

// nebulaResult carries the user's intent after a nebula execution completes.
type nebulaResult struct {
	Err          error  // execution error (if any)
	ReturnToHome bool   // user pressed Esc on overlay to return to home
	NextNebula   string // user selected a nebula from the picker
}

// runSelectedNebula loads, validates, and executes a single nebula in TUI
// mode. It delegates to the shared executeTUIRun helper for the TUI
// lifecycle, eliminating duplication with runApplyWithTUI.
func runSelectedNebula(cfg config.Config, printer *ui.Printer, dir string, noSplash bool, maxWorkers int, maxWorkersExplicit bool) nebulaResult {
	ecfg := engineConfigFromSettings(cfg, dir, noSplash, maxWorkers, maxWorkersExplicit)

	invoker := claude.NewInvoker(cfg.ClaudePath, cfg.Verbose)
	engine := nebula.NewEngine(ecfg, nil, invoker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load, validate, resolve workDir, create branch, load state.
	if err := engine.Load(ctx); err != nil {
		return nebulaResult{Err: handleLoadError(printer, err)}
	}

	// Build plan.
	if err := engine.Plan(ctx); err != nil {
		printer.Error(err.Error())
		return nebulaResult{Err: err}
	}

	if !engine.GetPlan().HasChanges() {
		printer.Info("nothing to do — all phases already applied")
		return nebulaResult{}
	}

	if err := engine.ApplyPlan(ctx); err != nil {
		printer.Error(err.Error())
		return nebulaResult{Err: err}
	}

	// Validate invoker before starting workers.
	if err := invoker.Validate(); err != nil {
		return nebulaResult{Err: fmt.Errorf("claude not available: %w", err)}
	}

	// Initialize fabric infrastructure.
	fc, fcErr := initFabric(ctx, engine.GetNebula(), dir, engine.WorkDir(), invoker)
	if fcErr != nil {
		return nebulaResult{Err: fmt.Errorf("fabric initialization failed: %w", fcErr)}
	}
	defer fc.Close()
	engine.SetFabric(fc)

	// Scan project context.
	projectCtx := scanProjectContext(ctx, engine.WorkDir())

	// Execute via shared TUI path.
	appModel, err := executeTUIRun(ctx, cancel, engine, invoker, ecfg, projectCtx)
	if err != nil {
		return nebulaResult{Err: err}
	}

	res := nebulaResult{
		ReturnToHome: appModel.ReturnToHome,
		NextNebula:   appModel.NextNebula,
	}
	if appModel.DoneErr != nil && !errors.Is(appModel.DoneErr, nebula.ErrManualStop) {
		res.Err = appModel.DoneErr
	}
	return res
}
