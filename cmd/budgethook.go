package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/loop"
)

// budgetHookCmd is the hidden entry point the Claude CLI invokes as a
// PreToolUse hook (wired by the claude invoker when a star/agent enables the
// tool budget). It reads a PreToolUse event on stdin, applies the per-tool
// Read/Grep budget persisted at --state (seeded from the cap flags on the
// first call of an invocation), and writes the CLI hook decision to stdout.
//
// It is hidden because operators never run it directly; it exists solely so the
// in-process loop.Budget can govern tools that execute inside the CLI
// subprocess, where Quasar has no other interception point.
var budgetHookCmd = &cobra.Command{
	Use:    "__budget-hook",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		statePath, _ := cmd.Flags().GetString("state")
		defaults := loop.Budget{
			MaxReadsBeforeEdit: mustInt(cmd, "max-reads-before-edit"),
			MaxGrepsBeforeEdit: mustInt(cmd, "max-greps-before-edit"),
			MaxTotalReads:      mustInt(cmd, "max-total-reads"),
			SoftAdvisory:       true,
		}
		return loop.RunToolHook(statePath, defaults, os.Stdin, os.Stdout) // arch-test: stdout-allowed — structured JSON hook decision for the Claude CLI, not human output
	},
}

// mustInt reads an int flag, returning 0 when unset or invalid.
func mustInt(cmd *cobra.Command, name string) int {
	v, _ := cmd.Flags().GetInt(name)
	return v
}

func init() {
	budgetHookCmd.Flags().String("state", "", "path to the per-invocation budget state file")
	budgetHookCmd.Flags().Int("max-reads-before-edit", 0, "soft Read-before-edit cap")
	budgetHookCmd.Flags().Int("max-greps-before-edit", 0, "soft Grep-before-edit cap")
	budgetHookCmd.Flags().Int("max-total-reads", 0, "hard total-Reads cap")
	rootCmd.AddCommand(budgetHookCmd)
}
