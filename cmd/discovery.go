package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/fabric"
)

var discoveryCmd = &cobra.Command{
	Use:   "discovery",
	Short: "Post an agent discovery to the fabric",
	Long: `Posts a discovery to the fabric database. Discoveries surface issues found
during execution — entanglement disputes, missing dependencies, file conflicts,
requirements ambiguities, and budget alerts.

Discoveries that qualify as hails (all kinds except budget_alert) pause the
affected tasks and surface for human attention in the cockpit.`,
	RunE: runDiscovery,
}

func init() {
	discoveryCmd.Flags().String("kind", "", "discovery kind: entanglement_dispute, missing_dependency, file_conflict, requirements_ambiguity, budget_alert (required)")
	discoveryCmd.Flags().String("detail", "", "free-text explanation of the discovery (required)")
	discoveryCmd.Flags().String("affects", "", "task ID affected by this discovery (optional; omit for broadcast)")
	discoveryCmd.Flags().String("task", os.Getenv("QUASAR_TASK_ID"), "source task posting the discovery (or QUASAR_TASK_ID env)")
	discoveryCmd.Flags().String("db", os.Getenv("QUASAR_FABRIC_DB"), "fabric database path (or QUASAR_FABRIC_DB env)")

	_ = discoveryCmd.MarkFlagRequired("kind")
	_ = discoveryCmd.MarkFlagRequired("detail")

	rootCmd.AddCommand(discoveryCmd)
}

func runDiscovery(cmd *cobra.Command, _ []string) error {
	kind, _ := cmd.Flags().GetString("kind")
	detail, _ := cmd.Flags().GetString("detail")
	affects, _ := cmd.Flags().GetString("affects")
	task, _ := cmd.Flags().GetString("task")
	dbPath, _ := cmd.Flags().GetString("db")

	// Validate discovery kind.
	if err := fabric.ValidateDiscoveryKind(kind); err != nil {
		return err
	}

	if dbPath == "" {
		return fmt.Errorf("fabric database path required: use --db or set QUASAR_FABRIC_DB")
	}

	ctx := cmd.Context()
	f, err := fabric.NewSQLiteFabric(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open fabric: %w", err)
	}
	defer f.Close()

	d := fabric.Discovery{
		SourceTask: task,
		Kind:       kind,
		Detail:     detail,
		Affects:    affects,
	}

	id, err := f.PostDiscovery(ctx, d)
	if err != nil {
		return fmt.Errorf("post discovery: %w", err)
	}

	// Print the discovery ID to stdout (structured output).
	fmt.Fprintln(cmd.OutOrStdout(), id)
	return nil
}
