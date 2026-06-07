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

// CacheMetric records prompt-cache token accounting for a single agent
// invocation. It is the unit of truth for verifying that Anthropic's prompt
// cache is actually hitting: a non-zero CacheRead on a repeat invocation of the
// same phase means the cached prefix was reused.
//
// JSON tags are kept short so each serialized line stays small (~100 bytes).
type CacheMetric struct {
	InvocationID  string    `json:"id"`
	NebulaID      string    `json:"neb"`
	PhaseID       string    `json:"phase"`
	CycleN        int       `json:"cycle"`
	InputTokens   int       `json:"in"`
	CacheCreate   int       `json:"create"`
	CacheRead     int       `json:"read"`
	CacheHitRatio float64   `json:"hit"`
	Timestamp     time.Time `json:"ts"`
}

// CacheHitRatio computes the fraction of input tokens served from the cache:
// read / (read + freshInput), clamped to [0,1]. It returns 0 when there were no
// input tokens at all (nothing to cache, so no meaningful ratio).
func CacheHitRatio(read, freshInput int) float64 {
	total := read + freshInput
	if total <= 0 {
		return 0
	}
	return float64(read) / float64(total)
}

// HitRatioFor aggregates a slice of metrics into a single pooled hit ratio.
// Pooling token counts (rather than averaging per-record ratios) weights each
// invocation by its size, which is the correct way to summarize cache savings.
func HitRatioFor(metrics []CacheMetric) float64 {
	var read, fresh int
	for _, m := range metrics {
		read += m.CacheRead
		fresh += m.InputTokens
	}
	return CacheHitRatio(read, fresh)
}

// CacheMetricStore appends CacheMetric records to a JSONL file and reads them
// back for reporting.
//
// Concurrency has two layers. Within a single store instance the mutex
// serializes Record calls, so concurrent goroutines (e.g. max_workers phases)
// are safe. Across *separate* store instances over the same file — including
// separate processes — the mutex does not apply; safety then rests on POSIX
// O_APPEND write atomicity, which holds only for writes below PIPE_BUF. See the
// invariant documented on Record.
type CacheMetricStore struct {
	path string
	mu   sync.Mutex
}

// NewCacheMetricStore returns a store backed by the JSONL file at path. The
// file and its parent directory are created lazily on the first Record.
func NewCacheMetricStore(path string) *CacheMetricStore {
	return &CacheMetricStore{path: path}
}

// Record appends a metric as one JSONL line. It fills in the derived
// CacheHitRatio and a UTC Timestamp when the caller left them zero.
func (s *CacheMetricStore) Record(ctx context.Context, metric CacheMetric) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if metric.Timestamp.IsZero() {
		metric.Timestamp = time.Now().UTC()
	}
	metric.CacheHitRatio = CacheHitRatio(metric.CacheRead, metric.InputTokens)

	line, err := json.Marshal(metric)
	if err != nil {
		return fmt.Errorf("telemetry: marshal cache metric: %w", err)
	}
	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("telemetry: create cache metric dir: %w", err)
		}
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("telemetry: open cache metrics %s: %w", s.path, err)
	}
	defer f.Close()

	// INVARIANT: cross-instance and cross-process interleaving safety depends on
	// this being a single O_APPEND write smaller than PIPE_BUF (4096 on Linux,
	// 512 on the POSIX minimum). CacheMetric serializes to well under that, and
	// callers must keep it so. If a future field pushes a line past PIPE_BUF, or
	// the log lands on a network filesystem without atomic append, concurrent
	// writers from different store instances could interleave and corrupt lines —
	// switch to file locking (flock) at that point.
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("telemetry: append cache metric: %w", err)
	}
	return nil
}

// MetricsByNebula returns every recorded metric for nebulaID in file order.
// A missing log file is not an error — it yields an empty slice.
func (s *CacheMetricStore) MetricsByNebula(ctx context.Context, nebulaID string) ([]CacheMetric, error) {
	all, err := s.readAll(ctx)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, m := range all {
		if m.NebulaID == nebulaID {
			out = append(out, m)
		}
	}
	return out, nil
}

// HitRateByPhase returns the pooled cache hit ratio per phase for nebulaID.
func (s *CacheMetricStore) HitRateByPhase(ctx context.Context, nebulaID string) (map[string]float64, error) {
	metrics, err := s.MetricsByNebula(ctx, nebulaID)
	if err != nil {
		return nil, err
	}
	return HitRateByPhaseFor(metrics), nil
}

// CycleHitRate pairs a cycle number with its pooled cache hit ratio. The cycle
// number is the real loop cycle (loops start at 1), not a slice index, so
// non-contiguous cycles are reported honestly rather than renumbered.
type CycleHitRate struct {
	Cycle   int
	HitRate float64
}

// HitRateByCycle returns the pooled hit ratio for each cycle of one phase,
// ordered by ascending cycle number. Multiple records sharing a cycle (e.g. a
// coder and reviewer invocation) are pooled into that cycle's ratio.
func (s *CacheMetricStore) HitRateByCycle(ctx context.Context, nebulaID, phaseID string) ([]CycleHitRate, error) {
	metrics, err := s.MetricsByNebula(ctx, nebulaID)
	if err != nil {
		return nil, err
	}
	return HitRateByCycleFor(metrics, phaseID), nil
}

// HitRateByPhaseFor computes the pooled hit ratio per phase from an in-memory
// slice of metrics (already scoped to one nebula). It performs no I/O, so
// callers that have already loaded the metrics avoid re-reading the log.
func HitRateByPhaseFor(metrics []CacheMetric) map[string]float64 {
	byPhase := make(map[string][]CacheMetric)
	for _, m := range metrics {
		byPhase[m.PhaseID] = append(byPhase[m.PhaseID], m)
	}
	rates := make(map[string]float64, len(byPhase))
	for phase, ms := range byPhase {
		rates[phase] = HitRatioFor(ms)
	}
	return rates
}

// HitRateByCycleFor computes the pooled hit ratio for each cycle of phaseID
// from an in-memory slice of metrics, ordered by ascending cycle number. Each
// result carries its real cycle number, so non-contiguous cycles are preserved
// rather than renumbered. It performs no I/O.
func HitRateByCycleFor(metrics []CacheMetric, phaseID string) []CycleHitRate {
	byCycle := make(map[int][]CacheMetric)
	for _, m := range metrics {
		if m.PhaseID != phaseID {
			continue
		}
		byCycle[m.CycleN] = append(byCycle[m.CycleN], m)
	}
	cycles := make([]int, 0, len(byCycle))
	for c := range byCycle {
		cycles = append(cycles, c)
	}
	sort.Ints(cycles)

	out := make([]CycleHitRate, 0, len(cycles))
	for _, c := range cycles {
		out = append(out, CycleHitRate{Cycle: c, HitRate: HitRatioFor(byCycle[c])})
	}
	return out
}

// readAll loads every metric line from the log. A non-existent file yields an
// empty slice rather than an error, so reporting works before any run.
func (s *CacheMetricStore) readAll(ctx context.Context) ([]CacheMetric, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("telemetry: open cache metrics %s: %w", s.path, err)
	}
	defer f.Close()

	var metrics []CacheMetric
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var m CacheMetric
		if err := json.Unmarshal(line, &m); err != nil {
			// Skip a corrupt line rather than abandoning the whole report.
			continue
		}
		metrics = append(metrics, m)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("telemetry: read cache metrics %s: %w", s.path, err)
	}
	return metrics, nil
}
