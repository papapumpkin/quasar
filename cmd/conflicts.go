package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/telemetry"
)

// conflictResolutionLogPath is the fixed location of the append-only
// conflict-resolution log.
var conflictResolutionLogPath = filepath.Join(".quasar", "telemetry", "conflict_resolutions.jsonl")

var conflictsCmd = &cobra.Command{
	Use:   "conflicts",
	Short: "Inspect cross-phase merge-conflict resolutions",
}

var conflictsReportCmd = &cobra.Command{
	Use:   "report",
	Short: "Report conflict-resolution rate, cost, and latency",
	Long: `Walks the conflict-resolution log and prints the resolution rate (resolved /
total), the average cost and latency per resolution, and the file paths most
often involved in conflicts — a signal for where the codebase has structural
cross-cutting concerns.

Use --since to bound the window (e.g. 24h, 7d-equivalent 168h).`,
	RunE: runConflictsReport,
}

func init() {
	conflictsReportCmd.Flags().Duration("since", 7*24*time.Hour, "only include resolutions newer than this window (e.g. 24h, 168h)")
	conflictsCmd.AddCommand(conflictsReportCmd)
	rootCmd.AddCommand(conflictsCmd)
}

func runConflictsReport(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	since, _ := cmd.Flags().GetDuration("since")
	cutoff := time.Now().Add(-since)

	log := telemetry.NewConflictResolutionLog(conflictResolutionLogPath)
	events, err := log.ReadSince(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("conflicts report: %w", err)
	}

	w := cmd.OutOrStdout()
	if len(events) == 0 {
		fmt.Fprintf(w, "no conflict resolutions in the last %s\n", since)
		return nil
	}

	rep := telemetry.AggregateConflictResolutions(events)

	fmt.Fprintf(w, "Conflict-resolution report (last %s)\n\n", since)
	fmt.Fprintf(w, "  Resolutions: %d total, %d resolved (%.0f%% resolution rate)\n",
		rep.Total, rep.Resolved, rep.ResolutionRate()*100)
	fmt.Fprintf(w, "  Average cost: $%.2f per resolution\n", rep.AvgCostUSD)
	fmt.Fprintf(w, "  Average latency: %.0f ms per resolution\n", rep.AvgLatencyMs)

	if len(rep.FilePaths) > 0 {
		fmt.Fprintln(w, "\n  Top files involved in conflicts:")
		for _, path := range sortedByCountDesc(rep.FilePaths) {
			fmt.Fprintf(w, "    %s: %d\n", path, rep.FilePaths[path])
		}
	}
	return nil
}
