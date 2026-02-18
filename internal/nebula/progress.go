package nebula

import (
	"fmt"
	"io"
)

// ProgressReporter handles progress reporting, checkpoint building,
// state persistence, and metrics recording for the worker group.
type ProgressReporter struct {
	state      *State
	nebula     *Nebula
	onProgress ProgressFunc
	metrics    *Metrics
	logger     io.Writer
}

// NewProgressReporter creates a ProgressReporter with the given dependencies.
func NewProgressReporter(nebula *Nebula, state *State, onProgress ProgressFunc, metrics *Metrics, logger io.Writer) *ProgressReporter {
	return &ProgressReporter{
		state:      state,
		nebula:     nebula,
		onProgress: onProgress,
		metrics:    metrics,
		logger:     logger,
	}
}

// ReportProgress calls the progress callback (if set) with current counts.
// Must be called with the WorkerGroup mutex held.
func (pr *ProgressReporter) ReportProgress() {
	if pr.onProgress == nil {
		return
	}
	total := len(pr.nebula.Phases)
	var completed, open, closed int
	for _, ps := range pr.state.Phases {
		switch ps.Status {
		case PhaseStatusDone:
			closed++
			completed++
		case PhaseStatusFailed:
			closed++
			completed++
		case PhaseStatusSkipped:
			closed++
			completed++
		case PhaseStatusInProgress, PhaseStatusCreated:
			open++
		case PhaseStatusPending:
			// Pending phases have no bead yet — not counted in open or closed.
			// They still contribute to total (via len(pr.nebula.Phases)).
		}
	}
	pr.onProgress(completed, total, open, closed, pr.state.TotalCostUSD)
}

// SaveState persists the current state to disk. Logs a warning on failure.
// Must be called with the WorkerGroup mutex held.
func (pr *ProgressReporter) SaveState() {
	if err := SaveState(pr.nebula.Dir, pr.state); err != nil {
		fmt.Fprintf(pr.logger, "warning: failed to save state: %v\n", err)
	}
}

// RecordPhaseStart records phase start metrics if metrics collection is enabled.
func (pr *ProgressReporter) RecordPhaseStart(phaseID string, waveNumber int) {
	if pr.metrics != nil {
		pr.metrics.RecordPhaseStart(phaseID, waveNumber)
	}
}

// RecordPhaseComplete records phase completion metrics if metrics collection is enabled.
func (pr *ProgressReporter) RecordPhaseComplete(phaseID string, result PhaseRunnerResult) {
	if pr.metrics != nil {
		pr.metrics.RecordPhaseComplete(phaseID, result)
	}
}

// RecordWaveComplete records wave completion metrics if metrics collection is enabled.
func (pr *ProgressReporter) RecordWaveComplete(waveNumber, effective, peak int) {
	if pr.metrics != nil {
		pr.metrics.RecordWaveComplete(waveNumber, effective, peak)
	}
}
