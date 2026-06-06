package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/repos"
	"github.com/papapumpkin/quasar/internal/sensors"
)

var lintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Validate artifact files (constellations, stars, skills, sensors)",
	Long: "Walks a repo's per-repo artifact directories and the embedded defaults, " +
		"validates each file against its schema, compiles every constellation " +
		"expression, and reports cross-reference and graph errors with file:line:col.",
	RunE: runLint,
}

func init() {
	lintCmd.Flags().String("repo", "", "repo path to lint (default: current directory)")
	lintCmd.Flags().Bool("strict", false, "treat unknown fields and warnings as errors")
	lintCmd.Flags().Bool("json", false, "emit findings as JSON to stdout")
	rootCmd.AddCommand(lintCmd)
}

func runLint(cmd *cobra.Command, _ []string) error {
	repoPath, _ := cmd.Flags().GetString("repo")
	strict, _ := cmd.Flags().GetBool("strict")
	asJSON, _ := cmd.Flags().GetBool("json")

	if repoPath == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("lint: resolve working directory: %w", err)
		}
		repoPath = wd
	}
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("lint: resolve %q: %w", repoPath, err)
	}

	resolver, err := repos.NewResolver(&repos.Repo{Path: abs})
	if err != nil {
		return fmt.Errorf("lint: %w", err)
	}

	loader := artifacts.New(resolver)
	loader.Strict = strict
	diags := loader.Lint(artifacts.LintOptions{
		SensorTypeKnown: sensors.Default().HasSensor,
	})

	if asJSON {
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(diags); err != nil {
			return fmt.Errorf("lint: encode json: %w", err)
		}
	} else {
		printDiagnostics(cmd, diags)
	}

	if lintFailed(diags, strict) {
		return newExitError(1, fmt.Errorf("lint found %d issue(s)", len(diags)))
	}
	return nil
}

// printDiagnostics renders findings to stderr, one per line, or a success line
// when there are none.
func printDiagnostics(cmd *cobra.Command, diags []artifacts.Diagnostic) {
	out := cmd.ErrOrStderr()
	if len(diags) == 0 {
		fmt.Fprintln(out, "lint: no issues found")
		return
	}
	for _, d := range diags {
		fmt.Fprintf(out, "%s: %s\n", d.Severity, d.Message)
	}
}

// lintFailed reports whether the diagnostics warrant a non-zero exit: any error,
// or — under strict — any warning.
func lintFailed(diags []artifacts.Diagnostic, strict bool) bool {
	for _, d := range diags {
		if d.Severity == artifacts.SevError || (strict && d.Severity == artifacts.SevWarning) {
			return true
		}
	}
	return false
}
