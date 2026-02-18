package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/ui"
)

var nebulaValidateCmd = &cobra.Command{
	Use:   "validate <path>",
	Short: "Validate a nebula directory structure and dependencies",
	Args:  cobra.ExactArgs(1),
	RunE:  runNebulaValidate,
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
