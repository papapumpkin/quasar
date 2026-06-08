package telemetry

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ConflictResolutionEvent is one row in the conflict-resolution log: the outcome
// of a single merge-conflict-resolve run. `quasar conflicts report` folds these
// into a resolution-rate / cost / latency summary and surfaces the file paths
// that collide most often.
//
// JSON tags are kept short so each serialized line stays well under PIPE_BUF
// (mirroring CoordinationEvent): cross-process O_APPEND atomicity holds only for
// small writes.
type ConflictResolutionEvent struct {
	Timestamp    time.Time `json:"ts"`
	SrcRun       string    `json:"src_run,omitempty"`
	DstRun       string    `json:"dst_run,omitempty"`
	Mode         string    `json:"mode"`
	Cycles       int       `json:"cycles"`
	Status       string    `json:"status"`
	FilesChanged int       `json:"files_changed"`
	Files        []string  `json:"files,omitempty"`
	LatencyMs    int64     `json:"latency_ms,omitempty"`
	CostUSD      float64   `json:"cost_usd,omitempty"`
}

// ConflictResolutionLog appends ConflictResolutionEvent rows to a JSONL file and
// reads them back for reporting. Concurrency mirrors CoordinationLog: the mutex
// serializes writes within one instance, and cross-instance safety rests on
// POSIX O_APPEND atomicity for sub-PIPE_BUF writes. A nil *ConflictResolutionLog
// is a no-op on every method so callers need not branch on whether telemetry is
// wired.
type ConflictResolutionLog struct {
	path string
	mu   sync.Mutex
}

// NewConflictResolutionLog returns a log backed by the JSONL file at path. The
// file and its parent directory are created lazily on the first write.
func NewConflictResolutionLog(path string) *ConflictResolutionLog {
	return &ConflictResolutionLog{path: path}
}

// Record appends one resolution-outcome row. A nil log is a no-op. The
// Timestamp is stamped (UTC) when the caller leaves it zero.
func (l *ConflictResolutionLog) Record(ctx context.Context, ev ConflictResolutionEvent) error {
	if l == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}

	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("telemetry: marshal conflict resolution event: %w", err)
	}
	line = append(line, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()

	if dir := filepath.Dir(l.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("telemetry: create conflict resolution log dir: %w", err)
		}
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("telemetry: open conflict resolution log %s: %w", l.path, err)
	}
	defer f.Close() //nolint:errcheck // append-only log; write error already surfaced

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("telemetry: append conflict resolution event: %w", err)
	}
	return nil
}

// ReadSince returns every event with a Timestamp at or after cutoff, in file
// order. A non-existent log yields an empty slice rather than an error, so
// reporting works before any resolution. Corrupt lines are skipped rather than
// abandoning the whole report. A zero cutoff returns all rows.
func (l *ConflictResolutionLog) ReadSince(ctx context.Context, cutoff time.Time) ([]ConflictResolutionEvent, error) {
	if l == nil {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("telemetry: open conflict resolution log %s: %w", l.path, err)
	}
	defer f.Close() //nolint:errcheck // read-only

	var events []ConflictResolutionEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev ConflictResolutionEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if !cutoff.IsZero() && ev.Timestamp.Before(cutoff) {
			continue
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("telemetry: read conflict resolution log %s: %w", l.path, err)
	}
	return events, nil
}

// ConflictResolutionReport aggregates conflict-resolution events for `quasar
// conflicts report`. Total counts every resolution attempt; Resolved counts the
// subset that ended status=resolved (the resolution rate is Resolved/Total).
// AvgCostUSD and AvgLatencyMs average across all attempts. FilePaths counts how
// often each file appears in a conflict, surfacing structural cross-cutting
// concerns.
type ConflictResolutionReport struct {
	Total        int
	Resolved     int
	AvgCostUSD   float64
	AvgLatencyMs float64
	FilePaths    map[string]int
}

// ResolutionRate returns Resolved/Total, or 0 when there were no attempts.
func (r ConflictResolutionReport) ResolutionRate() float64 {
	if r.Total == 0 {
		return 0
	}
	return float64(r.Resolved) / float64(r.Total)
}

// AggregateConflictResolutions folds a slice of events into a report. It
// performs no I/O, so a caller that already loaded the log avoids re-reading it.
func AggregateConflictResolutions(events []ConflictResolutionEvent) ConflictResolutionReport {
	rep := ConflictResolutionReport{FilePaths: map[string]int{}}
	var totalCost float64
	var totalLatency int64
	for _, ev := range events {
		rep.Total++
		if ev.Status == "resolved" {
			rep.Resolved++
		}
		totalCost += ev.CostUSD
		totalLatency += ev.LatencyMs
		for _, f := range ev.Files {
			rep.FilePaths[f]++
		}
	}
	if rep.Total > 0 {
		rep.AvgCostUSD = totalCost / float64(rep.Total)
		rep.AvgLatencyMs = float64(totalLatency) / float64(rep.Total)
	}
	return rep
}
