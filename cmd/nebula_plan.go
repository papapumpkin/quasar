package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/beads"
	"github.com/papapumpkin/quasar/internal/config"
	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/ui"
)

var nebulaPlanCmd = &cobra.Command{
	Use:   "plan <path>",
	Short: "Show what beads changes a nebula would produce",
	Args:  cobra.ExactArgs(1),
	RunE:  runNebulaPlan,
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
