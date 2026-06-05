package cmd

import (
	"github.com/spf13/cobra"
)

// validateCmd is a back-compat alias for `quasar doctor`. It was the
// dependency-check command before the integration/safety nebula folded those
// checks into the richer doctor report. Kept so existing scripts and docs that
// call `quasar validate` keep working, with a deprecation notice pointing users
// at the new command.
var validateCmd = &cobra.Command{
	Use:           "validate",
	Short:         "Alias for `quasar doctor`",
	Deprecated:    "use `quasar doctor` instead.",
	RunE:          runDoctor,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	validateCmd.Flags().Bool("json", false, "Emit the report as JSON on stdout")
	rootCmd.AddCommand(validateCmd)
}
