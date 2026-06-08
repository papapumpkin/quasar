package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/telemetry"
)

// healthEventsPath is the fixed location of the append-only health-events log.
var healthEventsPath = filepath.Join(".quasar", "telemetry", "health_events.jsonl")

var coderCmd = &cobra.Command{
	Use:   "coder",
	Short: "Inspect coder subprocess health and termination patterns",
}

var coderReportCmd = &cobra.Command{
	Use:   "report",
	Short: "Report dead-coder termination causes over a time window",
	Long: `Walks the health-events log and prints a histogram of termination
causes (which signals tripped degraded vs dead) over the given window.

Use --since to bound the window, e.g. --since 24h or --since 90m.`,
	RunE: runCoderReport,
}

func init() {
	coderReportCmd.Flags().Duration("since", 24*time.Hour, "only include events newer than this duration")
	coderCmd.AddCommand(coderReportCmd)
	rootCmd.AddCommand(coderCmd)
}

func runCoderReport(cmd *cobra.Command, _ []string) error {
	since, _ := cmd.Flags().GetDuration("since")
	ctx := cmd.Context()

	store := telemetry.NewHealthEventStore(healthEventsPath)
	cutoff := time.Now().Add(-since)
	events, err := store.Since(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("coder report: %w", err)
	}

	w := cmd.OutOrStdout()
	if len(events) == 0 {
		fmt.Fprintf(w, "no health events recorded in the last %s\n", since)
		return nil
	}

	hist := telemetry.TerminationHistogram(events)
	if len(hist) == 0 {
		fmt.Fprintf(w, "%d health events in the last %s, none terminal\n", len(events), since)
		return nil
	}

	fmt.Fprintf(w, "Coder health report (last %s)\n", since)
	for _, cause := range telemetry.SortedHistogram(hist) {
		fmt.Fprintf(w, "  %-22s %d\n", cause, hist[cause])
	}
	return nil
}
