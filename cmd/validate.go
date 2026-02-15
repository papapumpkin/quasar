package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/aaronsalm/quasar/internal/beads"
	"github.com/aaronsalm/quasar/internal/claude"
	"github.com/aaronsalm/quasar/internal/config"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Check that required dependencies are available",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.Load()
		ok := true

		claudeInv := &claude.Invoker{ClaudePath: cfg.ClaudePath, Verbose: cfg.Verbose}
		if err := claudeInv.Validate(); err != nil {
			fmt.Fprintf(os.Stderr, "✗ claude: %v\n", err)
			ok = false
		} else {
			fmt.Fprintln(os.Stderr, "✓ claude CLI found")
		}

		beadsClient := &beads.Client{BeadsPath: cfg.BeadsPath, Verbose: cfg.Verbose}
		if err := beadsClient.Validate(); err != nil {
			fmt.Fprintf(os.Stderr, "✗ beads: %v\n", err)
			ok = false
		} else {
			fmt.Fprintln(os.Stderr, "✓ beads CLI found")
		}

		if !ok {
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
}
