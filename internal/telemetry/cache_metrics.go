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
// back for reporting. It is safe for concurrent use: each Record performs a
// single atomic O_APPEND write under a mutex.
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
	byPhase := make(map[string][]CacheMetric)
	for _, m := range metrics {
		byPhase[m.PhaseID] = append(byPhase[m.PhaseID], m)
	}
	rates := make(map[string]float64, len(byPhase))
	for phase, ms := range byPhase {
		rates[phase] = HitRatioFor(ms)
	}
	return rates, nil
}

// HitRateByCycle returns the pooled hit ratio for each cycle of one phase,
// ordered by ascending cycle number. Multiple records sharing a cycle (e.g. a
// coder and reviewer invocation) are pooled into that cycle's ratio.
func (s *CacheMetricStore) HitRateByCycle(ctx context.Context, nebulaID, phaseID string) ([]float64, error) {
	metrics, err := s.MetricsByNebula(ctx, nebulaID)
	if err != nil {
		return nil, err
	}
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

	out := make([]float64, 0, len(cycles))
	for _, c := range cycles {
		out = append(out, HitRatioFor(byCycle[c]))
	}
	return out, nil
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
