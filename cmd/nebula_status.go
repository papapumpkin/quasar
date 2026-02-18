package cmd

import (
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/ui"
)

// addNebulaStatusFlags registers flags specific to the status subcommand.
func addNebulaStatusFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "output metrics as JSON to stdout")
}

func runNebulaStatus(cmd *cobra.Command, args []string) error {
	printer := ui.New()
	dir := args[0]

	n, err := nebula.Load(dir)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	state, err := nebula.LoadState(dir)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	metrics, history, err := nebula.LoadMetricsWithHistory(dir)
	if err != nil {
		printer.Error(err.Error())
		return err
	}

	jsonFlag, _ := cmd.Flags().GetBool("json")
	if jsonFlag {
		return writeStatusJSON(os.Stdout, n, state, metrics, history)
	}

	printer.NebulaStatus(n, state, metrics, history)
	return nil
}

// statusJSON is the structured representation of nebula status for --json output.
type statusJSON struct {
	Name        string            `json:"name"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
	TotalCost   float64           `json:"total_cost_usd"`
	TotalPhases int               `json:"total_phases"`
	Completed   int               `json:"completed"`
	Failed      int               `json:"failed"`
	Restarts    int               `json:"restarts"`
	Conflicts   int               `json:"conflicts"`
	DurationMs  int64             `json:"duration_ms,omitempty"`
	Waves       []statusWaveJSON  `json:"waves,omitempty"`
	Phases      []statusPhaseJSON `json:"phases,omitempty"`
	History     []statusRunJSON   `json:"history,omitempty"`
}

type statusWaveJSON struct {
	WaveNumber           int   `json:"wave_number"`
	PhaseCount           int   `json:"phase_count"`
	EffectiveParallelism int   `json:"effective_parallelism"`
	DurationMs           int64 `json:"duration_ms"`
	Conflicts            int   `json:"conflicts"`
}

type statusPhaseJSON struct {
	PhaseID      string  `json:"phase_id"`
	WaveNumber   int     `json:"wave_number"`
	DurationMs   int64   `json:"duration_ms"`
	CostUSD      float64 `json:"cost_usd"`
	CyclesUsed   int     `json:"cycles_used"`
	Restarts     int     `json:"restarts"`
	Satisfaction string  `json:"satisfaction,omitempty"`
	Conflict     bool    `json:"conflict"`
}

type statusRunJSON struct {
	StartedAt   time.Time `json:"started_at"`
	TotalPhases int       `json:"total_phases"`
	TotalCost   float64   `json:"total_cost_usd"`
	DurationMs  int64     `json:"duration_ms"`
	Conflicts   int       `json:"conflicts"`
}

// writeStatusJSON encodes the nebula status as JSON to the given writer.
func writeStatusJSON(w io.Writer, n *nebula.Nebula, state *nebula.State, m *nebula.Metrics, history []nebula.HistorySummary) error {
	out := statusJSON{
		Name:        n.Manifest.Nebula.Name,
		TotalPhases: len(n.Phases),
	}

	// Phase counts from state.
	for _, ps := range state.Phases {
		switch ps.Status {
		case nebula.PhaseStatusDone:
			out.Completed++
		case nebula.PhaseStatusFailed:
			out.Failed++
		}
	}

	// Cost from state as fallback.
	out.TotalCost = state.TotalCostUSD

	if m != nil {
		if !m.StartedAt.IsZero() {
			out.StartedAt = &m.StartedAt
		}
		if !m.CompletedAt.IsZero() {
			out.CompletedAt = &m.CompletedAt
		}
		if m.TotalCostUSD > 0 {
			out.TotalCost = m.TotalCostUSD
		}
		out.Restarts = m.TotalRestarts
		out.Conflicts = m.TotalConflicts

		if !m.StartedAt.IsZero() && !m.CompletedAt.IsZero() {
			out.DurationMs = m.CompletedAt.Sub(m.StartedAt).Milliseconds()
		}

		out.Waves = make([]statusWaveJSON, len(m.Waves))
		for i, w := range m.Waves {
			out.Waves[i] = statusWaveJSON{
				WaveNumber:           w.WaveNumber,
				PhaseCount:           w.PhaseCount,
				EffectiveParallelism: w.EffectiveParallelism,
				DurationMs:           w.TotalDuration.Milliseconds(),
				Conflicts:            w.Conflicts,
			}
		}

		out.Phases = make([]statusPhaseJSON, len(m.Phases))
		for i, p := range m.Phases {
			out.Phases[i] = statusPhaseJSON{
				PhaseID:      p.PhaseID,
				WaveNumber:   p.WaveNumber,
				DurationMs:   p.Duration.Milliseconds(),
				CostUSD:      p.CostUSD,
				CyclesUsed:   p.CyclesUsed,
				Restarts:     p.Restarts,
				Satisfaction: p.Satisfaction,
				Conflict:     p.Conflict,
			}
		}
	}

	out.History = make([]statusRunJSON, len(history))
	for i, h := range history {
		out.History[i] = statusRunJSON{
			StartedAt:   h.StartedAt,
			TotalPhases: h.TotalPhases,
			TotalCost:   h.TotalCostUSD,
			DurationMs:  h.Duration.Milliseconds(),
			Conflicts:   h.TotalConflicts,
		}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
