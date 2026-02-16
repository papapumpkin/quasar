package nebula

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

const metricsFileName = "metrics.toml"

// maxHistoryEntries is the maximum number of historical run summaries kept.
const maxHistoryEntries = 10

// metricsFile is the TOML-serializable representation of the metrics file.
// It contains the most recent run's metrics and a history of previous runs.
type metricsFile struct {
	Current metricsRecord    `toml:"current"`
	History []historySummary `toml:"history"`
}

// metricsRecord is the TOML-serializable form of a single run's metrics.
// time.Duration fields are stored as nanosecond int64 values since the
// TOML library does not natively support Go durations.
type metricsRecord struct {
	NebulaName     string        `toml:"nebula_name"`
	StartedAt      time.Time     `toml:"started_at"`
	CompletedAt    time.Time     `toml:"completed_at"`
	TotalCostUSD   float64       `toml:"total_cost_usd"`
	TotalPhases    int           `toml:"total_phases"`
	TotalWaves     int           `toml:"total_waves"`
	TotalConflicts int           `toml:"total_conflicts"`
	TotalRestarts  int           `toml:"total_restarts"`
	Phases         []phaseRecord `toml:"phases"`
	Waves          []waveRecord  `toml:"waves"`
}

// phaseRecord is the TOML-serializable form of PhaseMetrics.
type phaseRecord struct {
	PhaseID      string    `toml:"phase_id"`
	WaveNumber   int       `toml:"wave_number"`
	StartedAt    time.Time `toml:"started_at"`
	CompletedAt  time.Time `toml:"completed_at"`
	DurationNs   int64     `toml:"duration_ns"`
	CyclesUsed   int       `toml:"cycles_used"`
	CostUSD      float64   `toml:"cost_usd"`
	Restarts     int       `toml:"restarts"`
	LockWaitNs   int64     `toml:"lock_wait_ns"`
	Satisfaction string    `toml:"satisfaction,omitempty"`
	Conflict     bool      `toml:"conflict,omitempty"`
}

// waveRecord is the TOML-serializable form of WaveMetrics.
type waveRecord struct {
	WaveNumber           int   `toml:"wave_number"`
	EffectiveParallelism int   `toml:"effective_parallelism"`
	ActualParallelism    int   `toml:"actual_parallelism"`
	PhaseCount           int   `toml:"phase_count"`
	TotalDurationNs      int64 `toml:"total_duration_ns"`
	Conflicts            int   `toml:"conflicts"`
	ChangeVolume         int   `toml:"change_volume,omitempty"`
	ActiveClaims         int   `toml:"active_claims,omitempty"`
	AvgClaimAgeNs        int64 `toml:"avg_claim_age_ns,omitempty"`
}

// historySummary captures a condensed record of a previous run.
type historySummary struct {
	NebulaName     string    `toml:"nebula_name"`
	StartedAt      time.Time `toml:"started_at"`
	CompletedAt    time.Time `toml:"completed_at"`
	TotalCostUSD   float64   `toml:"total_cost_usd"`
	DurationNs     int64     `toml:"duration_ns"`
	TotalPhases    int       `toml:"total_phases"`
	TotalConflicts int       `toml:"total_conflicts"`
	TotalRestarts  int       `toml:"total_restarts"`
}

// SaveMetrics writes the current metrics snapshot to the nebula directory.
// If a previous metrics file exists, its current section is rotated into
// the history array (capped at maxHistoryEntries most recent entries).
func SaveMetrics(dir string, m *Metrics) error {
	snap := m.Snapshot()

	// Load existing file to preserve history.
	existing, err := loadMetricsFile(dir)
	if err != nil {
		return fmt.Errorf("loading existing metrics: %w", err)
	}

	record := metricsToRecord(snap)

	var history []historySummary
	if existing != nil {
		// Rotate the previous current into history.
		history = append(existing.History, recordToSummary(existing.Current))
	}

	// Cap history at maxHistoryEntries most recent.
	if len(history) > maxHistoryEntries {
		history = history[len(history)-maxHistoryEntries:]
	}

	file := metricsFile{
		Current: record,
		History: history,
	}

	data, err := toml.Marshal(file)
	if err != nil {
		return fmt.Errorf("marshaling metrics: %w", err)
	}

	path := filepath.Join(dir, metricsFileName)
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("writing temp metrics file: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming metrics file: %w", err)
	}

	return nil
}

// LoadMetrics reads metrics from the nebula directory.
// Returns a zero-value Metrics if no file exists (first run).
func LoadMetrics(dir string) (*Metrics, error) {
	file, err := loadMetricsFile(dir)
	if err != nil {
		return nil, err
	}
	if file == nil {
		return &Metrics{
			Phases: make([]PhaseMetrics, 0),
			Waves:  make([]WaveMetrics, 0),
		}, nil
	}
	return recordToMetrics(file.Current), nil
}

// HistorySummary is an exported snapshot of a previous nebula run.
type HistorySummary struct {
	NebulaName     string
	StartedAt      time.Time
	CompletedAt    time.Time
	TotalCostUSD   float64
	Duration       time.Duration
	TotalPhases    int
	TotalConflicts int
	TotalRestarts  int
}

// LoadMetricsWithHistory loads the current metrics and up to maxHistoryEntries
// previous run summaries from the nebula directory. If no metrics file exists,
// both return values are nil (no error).
func LoadMetricsWithHistory(dir string) (*Metrics, []HistorySummary, error) {
	file, err := loadMetricsFile(dir)
	if err != nil {
		return nil, nil, err
	}
	if file == nil {
		return nil, nil, nil
	}

	current := recordToMetrics(file.Current)

	history := make([]HistorySummary, len(file.History))
	for i, h := range file.History {
		history[i] = HistorySummary{
			NebulaName:     h.NebulaName,
			StartedAt:      h.StartedAt,
			CompletedAt:    h.CompletedAt,
			TotalCostUSD:   h.TotalCostUSD,
			Duration:       time.Duration(h.DurationNs),
			TotalPhases:    h.TotalPhases,
			TotalConflicts: h.TotalConflicts,
			TotalRestarts:  h.TotalRestarts,
		}
	}

	return current, history, nil
}

// loadMetricsFile reads and parses the raw metrics file.
// Returns nil, nil if the file does not exist.
func loadMetricsFile(dir string) (*metricsFile, error) {
	path := filepath.Join(dir, metricsFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading metrics file: %w", err)
	}

	var file metricsFile
	if err := toml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing metrics file: %w", err)
	}

	return &file, nil
}

// metricsToRecord converts an in-memory Metrics to the TOML-serializable form.
func metricsToRecord(m *Metrics) metricsRecord {
	phases := make([]phaseRecord, len(m.Phases))
	for i, p := range m.Phases {
		phases[i] = phaseRecord{
			PhaseID:      p.PhaseID,
			WaveNumber:   p.WaveNumber,
			StartedAt:    p.StartedAt,
			CompletedAt:  p.CompletedAt,
			DurationNs:   int64(p.Duration),
			CyclesUsed:   p.CyclesUsed,
			CostUSD:      p.CostUSD,
			Restarts:     p.Restarts,
			LockWaitNs:   int64(p.LockWaitTime),
			Satisfaction: p.Satisfaction,
			Conflict:     p.Conflict,
		}
	}

	waves := make([]waveRecord, len(m.Waves))
	for i, w := range m.Waves {
		waves[i] = waveRecord{
			WaveNumber:           w.WaveNumber,
			EffectiveParallelism: w.EffectiveParallelism,
			ActualParallelism:    w.ActualParallelism,
			PhaseCount:           w.PhaseCount,
			TotalDurationNs:      int64(w.TotalDuration),
			Conflicts:            w.Conflicts,
			ChangeVolume:         w.ChangeVolume,
			ActiveClaims:         w.ActiveClaims,
			AvgClaimAgeNs:        int64(w.AvgClaimAge),
		}
	}

	return metricsRecord{
		NebulaName:     m.NebulaName,
		StartedAt:      m.StartedAt,
		CompletedAt:    m.CompletedAt,
		TotalCostUSD:   m.TotalCostUSD,
		TotalPhases:    m.TotalPhases,
		TotalWaves:     m.TotalWaves,
		TotalConflicts: m.TotalConflicts,
		TotalRestarts:  m.TotalRestarts,
		Phases:         phases,
		Waves:          waves,
	}
}

// recordToMetrics converts the TOML-serializable form back to an in-memory Metrics.
func recordToMetrics(r metricsRecord) *Metrics {
	phases := make([]PhaseMetrics, len(r.Phases))
	for i, p := range r.Phases {
		phases[i] = PhaseMetrics{
			PhaseID:      p.PhaseID,
			WaveNumber:   p.WaveNumber,
			StartedAt:    p.StartedAt,
			CompletedAt:  p.CompletedAt,
			Duration:     time.Duration(p.DurationNs),
			CyclesUsed:   p.CyclesUsed,
			CostUSD:      p.CostUSD,
			Restarts:     p.Restarts,
			LockWaitTime: time.Duration(p.LockWaitNs),
			Satisfaction: p.Satisfaction,
			Conflict:     p.Conflict,
		}
	}

	waves := make([]WaveMetrics, len(r.Waves))
	for i, w := range r.Waves {
		waves[i] = WaveMetrics{
			WaveNumber:           w.WaveNumber,
			EffectiveParallelism: w.EffectiveParallelism,
			ActualParallelism:    w.ActualParallelism,
			PhaseCount:           w.PhaseCount,
			TotalDuration:        time.Duration(w.TotalDurationNs),
			Conflicts:            w.Conflicts,
			ChangeVolume:         w.ChangeVolume,
			ActiveClaims:         w.ActiveClaims,
			AvgClaimAge:          time.Duration(w.AvgClaimAgeNs),
		}
	}

	return &Metrics{
		NebulaName:     r.NebulaName,
		StartedAt:      r.StartedAt,
		CompletedAt:    r.CompletedAt,
		TotalCostUSD:   r.TotalCostUSD,
		TotalPhases:    r.TotalPhases,
		TotalWaves:     r.TotalWaves,
		TotalConflicts: r.TotalConflicts,
		TotalRestarts:  r.TotalRestarts,
		Phases:         phases,
		Waves:          waves,
	}
}

// recordToSummary extracts a condensed history entry from a full metrics record.
func recordToSummary(r metricsRecord) historySummary {
	var durationNs int64
	if !r.CompletedAt.IsZero() && !r.StartedAt.IsZero() {
		durationNs = int64(r.CompletedAt.Sub(r.StartedAt))
	}

	return historySummary{
		NebulaName:     r.NebulaName,
		StartedAt:      r.StartedAt,
		CompletedAt:    r.CompletedAt,
		TotalCostUSD:   r.TotalCostUSD,
		DurationNs:     durationNs,
		TotalPhases:    r.TotalPhases,
		TotalConflicts: r.TotalConflicts,
		TotalRestarts:  r.TotalRestarts,
	}
}
