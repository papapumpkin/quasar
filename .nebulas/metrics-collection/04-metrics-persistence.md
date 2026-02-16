+++
id = "metrics-persistence"
title = "Persist metrics alongside nebula state"
type = "feature"
priority = 2
depends_on = ["metrics-types"]
scope = ["internal/nebula/metrics_store.go"]
+++

## Problem

Metrics are collected in memory during a run but lost when the process exits. For post-run analysis, comparing runs, and feeding the future adaptive controller, metrics need to persist.

## Solution

Create `internal/nebula/metrics_store.go` with functions to save and load metrics to/from the nebula directory.

### File format

Store as TOML alongside `state.toml`:

```
.nebulas/my-nebula/
  nebula.toml
  state.toml
  metrics.toml      ← new
```

### Functions

```go
// SaveMetrics writes the current metrics snapshot to the nebula directory.
func SaveMetrics(dir string, m *Metrics) error

// LoadMetrics reads metrics from the nebula directory.
// Returns a zero Metrics if no file exists (first run).
func LoadMetrics(dir string) (*Metrics, error)
```

### Append behavior

Each run appends to a history. The metrics file contains:
- `[current]` — metrics from the most recent run
- `[[history]]` — array of previous run summaries (capped at 10 most recent)

History entries are summarized (total cost, duration, conflicts, restarts, phase count) — not the full per-phase breakdown.

### Integration point

`WorkerGroup.Run` calls `SaveMetrics` at the end of execution, alongside the existing `SaveState` call. Only writes if `wg.Metrics != nil`.

## Files to Modify

- `internal/nebula/metrics_store.go` — New file: SaveMetrics, LoadMetrics, history management

## Acceptance Criteria

- [ ] Metrics saved as TOML in nebula directory
- [ ] LoadMetrics returns zero value on first run (no file)
- [ ] History capped at 10 entries
- [ ] Round-trip: Save then Load produces equivalent data
- [ ] `go test ./internal/nebula/...` passes
- [ ] `go vet ./...` passes
