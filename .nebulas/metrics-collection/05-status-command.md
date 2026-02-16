+++
id = "status-command"
title = "Add 'quasar nebula status' command for live and historical metrics"
type = "feature"
priority = 2
depends_on = ["metrics-persistence"]
scope = ["cmd/nebula.go"]
+++

## Problem

There is no way to inspect metrics from a nebula run — neither during execution nor after completion. Users need visibility into phase durations, costs, conflicts, and parallelism efficiency.

## Solution

Add a `quasar nebula status <path>` subcommand that reads persisted metrics and renders a summary to stderr.

### Output format

```
nebula "local-parallelism" — last run 2026-02-15T10:30:00Z

  Phases:  8 completed, 0 failed, 2 restarts
  Waves:   3 (avg effective parallelism: 2.3)
  Cost:    $12.45 (avg $1.56/phase)
  Duration: 4m32s (wall-clock)
  Conflicts: 1

  Wave breakdown:
    Wave 1: 3 phases, parallelism 3/3, 1m12s
    Wave 2: 3 phases, parallelism 2/3 (scope serialization), 2m05s
    Wave 3: 2 phases, parallelism 2/2, 1m15s

  Slowest phases:
    scope-validation    1m45s  $3.20  3 cycles  satisfaction: high
    channel-coordinator 1m22s  $2.80  2 cycles  satisfaction: high
    worker-integration  1m10s  $2.50  4 cycles  satisfaction: medium

  History (last 3 runs):
    2026-02-15 10:30  8 phases  $12.45  4m32s  1 conflict
    2026-02-14 16:00  8 phases  $14.20  5m10s  3 conflicts
    2026-02-14 09:15  6 phases  $8.30   3m05s  0 conflicts
```

### Implementation

1. Add `nebulaStatusCmd` to `cmd/nebula.go`
2. Load state and metrics from the nebula directory
3. Render via `ui.Printer` to stderr
4. If no metrics file exists, show state-only summary (phase statuses, cost from state)

### Flags

- `--json` — output metrics as JSON to stdout (machine-readable, follows stdout convention)

## Files to Modify

- `cmd/nebula.go` — Add nebulaStatusCmd with rendering logic

## Acceptance Criteria

- [ ] `quasar nebula status .nebulas/foo` renders metrics summary
- [ ] Graceful fallback when no metrics.toml exists
- [ ] `--json` outputs to stdout as structured JSON
- [ ] All output (non-JSON) goes to stderr
- [ ] `go build ./...` compiles
- [ ] `go vet ./...` passes
