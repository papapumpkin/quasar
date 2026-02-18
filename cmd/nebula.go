package cmd

import (
	"github.com/spf13/cobra"
)

var nebulaCmd = &cobra.Command{
	Use:   "nebula",
	Short: "Manage nebula blueprints (validate, plan, apply, show, status)",
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
