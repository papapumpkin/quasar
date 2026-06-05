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
	long  string // extended help; empty falls back to short
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
		short: "Import a nebula blueprint into SQLite and execute its phases",
		long: "Parse a nebula directory (nebula.toml + *.md phase files) and import it " +
			"into the SQLite-canonical store, then execute its phases.\n\n" +
			"PREREQUISITE: the nebula is recorded under the registered repo that owns " +
			"the current working directory, so the CWD must be inside a repo registered " +
			"with `quasar repo register <path>`. If no repo is registered for the CWD, " +
			"apply exits with an error before doing any work. The on-disk files are left " +
			"untouched — they remain a valid authoring surface, but SQLite is the source " +
			"of execution.",
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
	{
		use:   "generate <prompt>",
		short: "Generate a complete nebula from a natural-language description",
		args:  cobra.ExactArgs(1),
		flags: addNebulaGenerateFlags,
		run:   runNebulaGenerate,
	},
}

func init() {
	for _, sc := range nebulaSubcmds {
		cmd := &cobra.Command{
			Use:   sc.use,
			Short: sc.short,
			Long:  sc.long,
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
