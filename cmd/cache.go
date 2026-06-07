package cmd

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/telemetry"
)

// cacheMetricsPath is the fixed location of the append-only cache metrics log.
var cacheMetricsPath = filepath.Join(".quasar", "telemetry", "cache_metrics.jsonl")

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Inspect prompt-cache effectiveness",
}

var cacheReportCmd = &cobra.Command{
	Use:   "report",
	Short: "Report prompt-cache hit rates for a nebula",
	Long: `Walks the cache metrics log and prints per-phase and per-cycle hit
rates plus a global average for the given nebula.

A non-zero cache_read on the second invocation of the same phase confirms the
prompt cache is being reused.`,
	RunE: runCacheReport,
}

func init() {
	cacheReportCmd.Flags().String("nebula", "", "nebula ID to report on (required)")
	if err := cacheReportCmd.MarkFlagRequired("nebula"); err != nil {
		panic(err)
	}
	cacheCmd.AddCommand(cacheReportCmd)
	rootCmd.AddCommand(cacheCmd)
}

func runCacheReport(cmd *cobra.Command, _ []string) error {
	nebulaID, _ := cmd.Flags().GetString("nebula")
	ctx := cmd.Context()

	store := telemetry.NewCacheMetricStore(cacheMetricsPath)
	metrics, err := store.MetricsByNebula(ctx, nebulaID)
	if err != nil {
		return fmt.Errorf("cache report: %w", err)
	}

	w := cmd.OutOrStdout()
	if len(metrics) == 0 {
		fmt.Fprintf(w, "no cache metrics recorded for nebula %q\n", nebulaID)
		return nil
	}

	fmt.Fprintf(w, "Cache report for nebula %s\n", nebulaID)
	fmt.Fprintf(w, "  global hit rate: %s\n\n", formatPct(telemetry.HitRatioFor(metrics)))

	phaseRates, err := store.HitRateByPhase(ctx, nebulaID)
	if err != nil {
		return fmt.Errorf("cache report: %w", err)
	}

	for _, phase := range sortedPhases(phaseRates) {
		fmt.Fprintf(w, "  phase %s: %s\n", phase, formatPct(phaseRates[phase]))

		cycleRates, err := store.HitRateByCycle(ctx, nebulaID, phase)
		if err != nil {
			return fmt.Errorf("cache report: %w", err)
		}
		for i, rate := range cycleRates {
			fmt.Fprintf(w, "    cycle %d: %s\n", i, formatPct(rate))
		}
	}
	return nil
}

// sortedPhases returns the phase IDs of rates in deterministic (alphabetical)
// order for stable report output.
func sortedPhases(rates map[string]float64) []string {
	phases := make([]string, 0, len(rates))
	for p := range rates {
		phases = append(phases, p)
	}
	sort.Strings(phases)
	return phases
}

// formatPct renders a 0..1 ratio as a whole-number percentage.
func formatPct(ratio float64) string {
	return fmt.Sprintf("%d%%", int(ratio*100+0.5))
}
