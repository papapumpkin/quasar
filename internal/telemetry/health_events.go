package telemetry

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Health event names recorded in the health-events log. They mirror the
// healthcheck state machine: a coder degrades when one signal goes red, dies
// when two go red (or the wall-clock cap is hit), and the termination handshake
// (sigterm/sigkill/exited) is recorded so post-mortems can see whether the
// subprocess exited gracefully.
const (
	HealthEventDegraded = "degraded"
	HealthEventDead     = "dead"
	HealthEventSigterm  = "sigterm_sent"
	HealthEventSigkill  = "sigkill_sent"
	HealthEventExited   = "exited"
)

// HealthEvent is one record in the health-events JSONL log. A single event
// carries either one signal (degraded) or several (dead), plus optional
// numeric/elapsed context. JSON tags are kept short so each line stays small.
type HealthEvent struct {
	Timestamp    time.Time `json:"ts"`
	InvocationID string    `json:"id"`
	Event        string    `json:"event"`
	Signal       string    `json:"signal,omitempty"`
	Signals      []string  `json:"signals,omitempty"`
	Value        float64   `json:"value,omitempty"`
	Elapsed      string    `json:"elapsed,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	Clean        bool      `json:"clean,omitempty"`
}

// HealthEventStore appends HealthEvent records to a JSONL file and reads them
// back for `quasar coder report`. Concurrency mirrors CacheMetricStore: the
// mutex serializes Record within one instance, and cross-instance safety rests
// on POSIX O_APPEND atomicity for sub-PIPE_BUF writes.
type HealthEventStore struct {
	path string
	mu   sync.Mutex
}

// NewHealthEventStore returns a store backed by the JSONL file at path. The file
// and its parent directory are created lazily on the first Record.
func NewHealthEventStore(path string) *HealthEventStore {
	return &HealthEventStore{path: path}
}

// Record appends an event as one JSONL line, stamping a UTC Timestamp when the
// caller left it zero. A nil store is a no-op so callers need not branch.
func (s *HealthEventStore) Record(ctx context.Context, evt HealthEvent) error {
	if s == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}

	line, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("telemetry: marshal health event: %w", err)
	}
	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("telemetry: create health events dir: %w", err)
		}
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("telemetry: open health events %s: %w", s.path, err)
	}
	defer f.Close()

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("telemetry: append health event: %w", err)
	}
	return nil
}

// Since returns every event recorded at or after cutoff, in file order. A
// missing log file is not an error — it yields an empty slice.
func (s *HealthEventStore) Since(ctx context.Context, cutoff time.Time) ([]HealthEvent, error) {
	all, err := s.readAll(ctx)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, e := range all {
		if !e.Timestamp.Before(cutoff) {
			out = append(out, e)
		}
	}
	return out, nil
}

// TerminationHistogram counts terminal events by cause from a slice of events.
// The keys are signal names (for dead events, each contributing signal is
// counted) plus "degraded" totals keyed by their single signal. It is the
// aggregate behind `quasar coder report`.
func TerminationHistogram(events []HealthEvent) map[string]int {
	hist := make(map[string]int)
	for _, e := range events {
		switch e.Event {
		case HealthEventDead:
			if len(e.Signals) == 0 {
				hist["dead:unknown"]++
				continue
			}
			for _, sig := range e.Signals {
				hist["dead:"+sig]++
			}
		case HealthEventDegraded:
			hist["degraded:"+e.Signal]++
		}
	}
	return hist
}

// SortedHistogram returns the histogram keys in descending count order (ties
// broken alphabetically) so report output is deterministic.
func SortedHistogram(hist map[string]int) []string {
	keys := make([]string, 0, len(hist))
	for k := range hist {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if hist[keys[i]] != hist[keys[j]] {
			return hist[keys[i]] > hist[keys[j]]
		}
		return keys[i] < keys[j]
	})
	return keys
}

// readAll loads every event line from the log. A non-existent file yields an
// empty slice rather than an error, so reporting works before any run. Corrupt
// lines are skipped rather than abandoning the whole report.
func (s *HealthEventStore) readAll(ctx context.Context) ([]HealthEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("telemetry: open health events %s: %w", s.path, err)
	}
	defer f.Close()

	var events []HealthEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e HealthEvent
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		events = append(events, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("telemetry: read health events %s: %w", s.path, err)
	}
	return events, nil
}
