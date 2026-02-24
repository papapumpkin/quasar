package cmd

import (
	"github.com/spf13/cobra"
)

var nebulaCmd = &cobra.Command{
	Use:   "nebula",
	Short: "Manage nebula blueprints (validate, plan, apply, show, status)",
}

// nebulaSubcmd describes one subcommand under `quasar nebula`.
type nebulaSubcmd struct {
	use   string
	short string
	args  cobra.PositionalArgs
	flags func(cmd *cobra.Command) // registers command-specific flags; nil if none
	run   func(cmd *cobra.Command, args []string) error
}

// nebulaSubcmds is the table of all nebula subcommands.
var nebulaSubcmds = []nebulaSubcmd{
	{
		use:   "validate <path>",
		short: "Validate a nebula directory structure and dependencies",
		args:  cobra.ExactArgs(1),
		run:   runNebulaValidate,
	},
	{
		use:   "plan <path>",
		short: "Preview the execution plan for a nebula",
		args:  cobra.ExactArgs(1),
		flags: addNebulaPlanFlags,
		run:   runNebulaPlan,
	},
	{
		use:   "apply <path>",
		short: "Create/update beads from a nebula blueprint",
		args:  cobra.ExactArgs(1),
		flags: addNebulaApplyFlags,
		run:   runNebulaApply,
	},
	{
		use:   "show <path>",
		short: "Display current nebula state",
		args:  cobra.ExactArgs(1),
		run:   runNebulaShow,
	},
	{
		use:   "status <path>",
		short: "Display metrics summary for a nebula run",
		args:  cobra.ExactArgs(1),
		flags: addNebulaStatusFlags,
		run:   runNebulaStatus,
	},
}

func init() {
	for _, sc := range nebulaSubcmds {
		cmd := &cobra.Command{
			Use:   sc.use,
			Short: sc.short,
			Args:  sc.args,
			RunE:  sc.run,
		}
		if sc.flags != nil {
			sc.flags(cmd)
		}
		nebulaCmd.AddCommand(cmd)
	}
	rootCmd.AddCommand(nebulaCmd)
}
