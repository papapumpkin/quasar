package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/beads"
	"github.com/papapumpkin/quasar/internal/claude"
	"github.com/papapumpkin/quasar/internal/config"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Check that required dependencies are available",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		ok := true

		claudeInv := claude.NewInvoker(cfg.ClaudePath, cfg.Verbose)
		if err := claudeInv.Validate(); err != nil {
			fmt.Fprintf(os.Stderr, "✗ claude: %v\n", err)
			ok = false
		} else {
			fmt.Fprintln(os.Stderr, "✓ claude CLI found")
		}

		beadsClient := &beads.CLI{BeadsPath: cfg.BeadsPath, Verbose: cfg.Verbose}
		if err := beadsClient.Validate(); err != nil {
			fmt.Fprintf(os.Stderr, "✗ beads: %v\n", err)
			ok = false
		} else {
			fmt.Fprintln(os.Stderr, "✓ beads CLI found")
		}

		if !ok {
			os.Exit(1)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
}
