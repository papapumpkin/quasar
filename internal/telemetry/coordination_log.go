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

// Coordination event types written to the coordination log. A single file holds
// both shapes, discriminated by the Type field, so `quasar coordination report`
// reads one log to compute both note volume and override frequency.
const (
	CoordinationEventCheck    = "check"
	CoordinationEventOverride = "override"
)

// CoordinationEvent is one row in the coordination log. Two shapes share the
// struct, selected by Type:
//
//   - "check": one row per pre-flight coordination check. NotesCount and
//     ByStatus summarize the notes surfaced to the coder for PhaseID.
//   - "override": one row per note suppressed by a phase's [coordination]
//     ignore_deprecations / ignore_signatures allowlist. Symbol and Reason
//     record which note was dropped and why, the audit trail for how often
//     advisory coordination is deliberately ignored.
//
// JSON tags are kept short so each serialized line stays well under PIPE_BUF
// (see the invariant on CacheMetricStore.Record): cross-process O_APPEND
// atomicity holds only for small writes.
type CoordinationEvent struct {
	Type       string         `json:"type"`
	Timestamp  time.Time      `json:"ts"`
	RunID      string         `json:"run_id,omitempty"`
	PhaseID    string         `json:"phase_id"`
	NotesCount int            `json:"notes_count,omitempty"`
	ByStatus   map[string]int `json:"by_status,omitempty"`
	Symbol     string         `json:"symbol,omitempty"`
	Reason     string         `json:"reason,omitempty"`
}

// CoordinationLog appends CoordinationEvent rows to a JSONL file and reads them
// back for reporting. Concurrency mirrors CacheMetricStore: the mutex serializes
// writes within one instance, and cross-instance safety rests on POSIX O_APPEND
// atomicity for sub-PIPE_BUF writes. A nil *CoordinationLog is a no-op on every
// method so callers (e.g. the constellation Check) need not branch on whether
// telemetry is wired.
type CoordinationLog struct {
	path string
	mu   sync.Mutex
}

// NewCoordinationLog returns a log backed by the JSONL file at path. The file
// and its parent directory are created lazily on the first write.
func NewCoordinationLog(path string) *CoordinationLog {
	return &CoordinationLog{path: path}
}

// RecordCheck appends one "check" row summarizing a pre-flight coordination
// check: how many notes fired for the phase, broken down by status. A nil log is
// a no-op.
func (l *CoordinationLog) RecordCheck(ctx context.Context, runID, phaseID string, notesCount int, byStatus map[string]int) error {
	if l == nil {
		return nil
	}
	return l.append(ctx, CoordinationEvent{
		Type:       CoordinationEventCheck,
		RunID:      runID,
		PhaseID:    phaseID,
		NotesCount: notesCount,
		ByStatus:   byStatus,
	})
}

// RecordOverride appends one "override" row recording a note a phase's spec
// deliberately suppressed. A nil log is a no-op.
func (l *CoordinationLog) RecordOverride(ctx context.Context, phaseID, symbol, reason string) error {
	if l == nil {
		return nil
	}
	return l.append(ctx, CoordinationEvent{
		Type:    CoordinationEventOverride,
		PhaseID: phaseID,
		Symbol:  symbol,
		Reason:  reason,
	})
}

// append writes one event as a JSONL line, stamping a UTC Timestamp when unset.
func (l *CoordinationLog) append(ctx context.Context, ev CoordinationEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}

	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("telemetry: marshal coordination event: %w", err)
	}
	line = append(line, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()

	if dir := filepath.Dir(l.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("telemetry: create coordination log dir: %w", err)
		}
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("telemetry: open coordination log %s: %w", l.path, err)
	}
	defer f.Close() //nolint:errcheck // append-only log; write error already surfaced

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("telemetry: append coordination event: %w", err)
	}
	return nil
}

// ReadSince returns every coordination event with a Timestamp at or after
// cutoff, in file order. A non-existent log yields an empty slice rather than an
// error, so reporting works before any run. Corrupt lines are skipped rather
// than abandoning the whole report. A zero cutoff returns all rows.
func (l *CoordinationLog) ReadSince(ctx context.Context, cutoff time.Time) ([]CoordinationEvent, error) {
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
		return nil, fmt.Errorf("telemetry: open coordination log %s: %w", l.path, err)
	}
	defer f.Close() //nolint:errcheck // read-only

	var events []CoordinationEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev CoordinationEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if !cutoff.IsZero() && ev.Timestamp.Before(cutoff) {
			continue
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("telemetry: read coordination log %s: %w", l.path, err)
	}
	return events, nil
}

// CoordinationReport aggregates coordination events for `quasar coordination
// report`. NotesByPhase sums note volume per phase; OverridesByPhase counts
// suppressed notes per phase; ContendedSymbols counts override rows per symbol
// so the most-contended symbols can be surfaced.
type CoordinationReport struct {
	NotesByPhase     map[string]int
	OverridesByPhase map[string]int
	ContendedSymbols map[string]int
}

// AggregateCoordination folds a slice of events into a CoordinationReport. It
// performs no I/O, so a caller that already loaded the log avoids re-reading it.
func AggregateCoordination(events []CoordinationEvent) CoordinationReport {
	rep := CoordinationReport{
		NotesByPhase:     map[string]int{},
		OverridesByPhase: map[string]int{},
		ContendedSymbols: map[string]int{},
	}
	for _, ev := range events {
		switch ev.Type {
		case CoordinationEventCheck:
			rep.NotesByPhase[ev.PhaseID] += ev.NotesCount
		case CoordinationEventOverride:
			rep.OverridesByPhase[ev.PhaseID]++
			if ev.Symbol != "" {
				rep.ContendedSymbols[ev.Symbol]++
			}
		}
	}
	return rep
}
