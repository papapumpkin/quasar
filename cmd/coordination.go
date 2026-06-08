package cmd

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/telemetry"
)

// coordinationLogPath is the fixed location of the append-only coordination log.
var coordinationLogPath = filepath.Join(".quasar", "telemetry", "coordination_log.jsonl")

var coordinationCmd = &cobra.Command{
	Use:   "coordination",
	Short: "Inspect cross-phase coordination activity",
}

var coordinationReportCmd = &cobra.Command{
	Use:   "report",
	Short: "Report coordination-note volume and override usage",
	Long: `Walks the coordination log and prints, per phase, how often coordination
notes fired and how often a phase overrode them, plus the symbols that caused
the most cross-phase contention.

Use --since to bound the window (e.g. 24h, 7d-equivalent 168h). A high override
rate means coders are routinely ignoring sibling advice — worth auditing.`,
	RunE: runCoordinationReport,
}

func init() {
	coordinationReportCmd.Flags().Duration("since", 24*time.Hour, "only include events newer than this window (e.g. 24h, 1h30m)")
	coordinationCmd.AddCommand(coordinationReportCmd)
	rootCmd.AddCommand(coordinationCmd)
}

func runCoordinationReport(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	since, _ := cmd.Flags().GetDuration("since")
	cutoff := time.Now().Add(-since)

	log := telemetry.NewCoordinationLog(coordinationLogPath)
	events, err := log.ReadSince(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("coordination report: %w", err)
	}

	w := cmd.OutOrStdout()
	if len(events) == 0 {
		fmt.Fprintf(w, "no coordination activity in the last %s\n", since)
		return nil
	}

	rep := telemetry.AggregateCoordination(events)

	fmt.Fprintf(w, "Coordination report (last %s)\n\n", since)

	fmt.Fprintln(w, "  Notes fired per phase:")
	for _, phase := range sortedKeys(rep.NotesByPhase) {
		overrides := rep.OverridesByPhase[phase]
		fmt.Fprintf(w, "    %s: %d notes, %d overridden\n", phase, rep.NotesByPhase[phase], overrides)
	}

	// A phase may only appear in the override map (e.g. all of its notes were
	// suppressed before a summary note count accrued); surface those too.
	for _, phase := range sortedKeys(rep.OverridesByPhase) {
		if _, seen := rep.NotesByPhase[phase]; seen {
			continue
		}
		fmt.Fprintf(w, "    %s: 0 notes, %d overridden\n", phase, rep.OverridesByPhase[phase])
	}

	if len(rep.ContendedSymbols) > 0 {
		fmt.Fprintln(w, "\n  Most-contended symbols (by override count):")
		for _, sym := range sortedByCountDesc(rep.ContendedSymbols) {
			fmt.Fprintf(w, "    %s: %d\n", sym, rep.ContendedSymbols[sym])
		}
	}
	return nil
}

// sortedKeys returns the keys of a string→int map in deterministic
// (alphabetical) order for stable report output.
func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedByCountDesc returns a map's keys ordered by descending count, breaking
// ties alphabetically so output is deterministic.
func sortedByCountDesc(m map[string]int) []string {
	keys := sortedKeys(m)
	sort.SliceStable(keys, func(i, j int) bool {
		return m[keys[i]] > m[keys[j]]
	})
	return keys
}
