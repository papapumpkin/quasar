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

// routerMetricsPath is the fixed location of the append-only router metrics log.
var routerMetricsPath = filepath.Join(".quasar", "telemetry", "router_metrics.jsonl")

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
	cacheReportCmd.Flags().Bool("router", false, "report model-routing token savings instead of cache hit rates")
	if err := cacheReportCmd.MarkFlagRequired("nebula"); err != nil {
		panic(err)
	}
	cacheCmd.AddCommand(cacheReportCmd)
	rootCmd.AddCommand(cacheCmd)
}

func runCacheReport(cmd *cobra.Command, _ []string) error {
	nebulaID, _ := cmd.Flags().GetString("nebula")
	if router, _ := cmd.Flags().GetBool("router"); router {
		return runRouterReport(cmd, nebulaID)
	}
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

	// Compute every aggregate from the single slice already loaded above, so the
	// append-only log is parsed exactly once regardless of phase count.
	phaseRates := telemetry.HitRateByPhaseFor(metrics)
	for _, phase := range sortedPhases(phaseRates) {
		fmt.Fprintf(w, "  phase %s: %s\n", phase, formatPct(phaseRates[phase]))

		for _, c := range telemetry.HitRateByCycleFor(metrics, phase) {
			fmt.Fprintf(w, "    cycle %d: %s\n", c.Cycle, formatPct(c.HitRate))
		}
	}
	return nil
}

// runRouterReport prints the estimated tokens-saved-at-the-premium-tier per
// phase: every routed sub-question ran on Haiku instead of the coder's
// Opus/Sonnet tier, so the per-phase sum of routed tokens is the volume that
// bypassed premium inference.
func runRouterReport(cmd *cobra.Command, nebulaID string) error {
	ctx := cmd.Context()

	store := telemetry.NewRouterMetricStore(routerMetricsPath)
	metrics, err := store.RouterMetricsByNebula(ctx, nebulaID)
	if err != nil {
		return fmt.Errorf("router report: %w", err)
	}

	w := cmd.OutOrStdout()
	if len(metrics) == 0 {
		fmt.Fprintf(w, "no router metrics recorded for nebula %q\n", nebulaID)
		return nil
	}

	savings := telemetry.RouterSavingsByPhaseFor(metrics)
	var total int
	for _, s := range savings {
		total += s
	}

	fmt.Fprintf(w, "Router savings report for nebula %s\n", nebulaID)
	fmt.Fprintf(w, "  estimated tokens not spent at premium tier: %d (across %d routed questions)\n\n", total, len(metrics))
	for _, phase := range sortedSavings(savings) {
		fmt.Fprintf(w, "  phase %s: ~%d tokens saved\n", phase, savings[phase])
	}
	return nil
}

// sortedSavings returns the phase IDs of savings in deterministic (alphabetical)
// order for stable report output.
func sortedSavings(savings map[string]int) []string {
	phases := make([]string, 0, len(savings))
	for p := range savings {
		phases = append(phases, p)
	}
	sort.Strings(phases)
	return phases
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
