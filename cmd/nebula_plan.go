package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/ui"
)

// addNebulaPlanFlags registers flags specific to the plan subcommand.
func addNebulaPlanFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "output the plan as JSON to stdout")
	cmd.Flags().Bool("save", false, "save the plan to <nebula-dir>/<name>.plan.json")
	cmd.Flags().Bool("diff", false, "diff against a previously saved plan")
	cmd.Flags().Bool("no-color", false, "disable ANSI colors in output")
}

func runNebulaPlan(cmd *cobra.Command, args []string) error {
	printer := ui.New()
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

	// Resolve working directory for the static scanner.
	workDir := n.Manifest.Context.WorkingDir
	if workDir == "" {
		workDir = "."
	}
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return fmt.Errorf("resolving work dir: %w", err)
	}

	pe := &nebula.PlanEngine{
		Scanner: &fabric.StaticScanner{WorkDir: absWorkDir},
	}

	ep, err := pe.Plan(n)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	// Read flags.
	jsonFlag, _ := cmd.Flags().GetBool("json")
	saveFlag, _ := cmd.Flags().GetBool("save")
	diffFlag, _ := cmd.Flags().GetBool("diff")
	noColor, _ := cmd.Flags().GetBool("no-color")

	// Handle --diff: compare against a previously saved plan.
	if diffFlag {
		planPath := filepath.Join(dir, ep.Name+".plan.json")
		oldPlan, loadErr := nebula.LoadPlan(planPath)
		if loadErr != nil {
			printer.Error(fmt.Sprintf("no previous plan found at %s: %v", planPath, loadErr))
			return fmt.Errorf("loading previous plan: %w", loadErr)
		}
		changes := nebula.Diff(oldPlan, ep)
		printer.ExecutionPlanDiff(ep.Name, changes, noColor)
		return nil
	}

	// Handle --json: emit structured JSON to stdout.
	if jsonFlag {
		return writePlanJSON(os.Stdout, ep)
	}

	// Default: human-readable output to stderr.
	printer.ExecutionPlanRender(ep, noColor)

	// Handle --save: persist plan to disk.
	if saveFlag {
		planPath := filepath.Join(dir, ep.Name+".plan.json")
		if err := ep.Save(planPath); err != nil {
			printer.Error(err.Error())
			return err
		}
		printer.ExecutionPlanSaved(planPath)
	}

	// Non-zero exit if error-severity risks detected.
	if hasErrorRisks(ep.Risks) {
		return nebula.ErrPlanHasErrors
	}

	return nil
}

// writePlanJSON encodes the execution plan as indented JSON to the given writer.
func writePlanJSON(w *os.File, ep *nebula.ExecutionPlan) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(ep); err != nil {
		return fmt.Errorf("encoding plan JSON: %w", err)
	}
	return nil
}

// hasErrorRisks returns true if any risk has error severity.
func hasErrorRisks(risks []nebula.PlanRisk) bool {
	for _, r := range risks {
		if r.Severity == "error" {
			return true
		}
	}
	return false
}
