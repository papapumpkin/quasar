+++
id = "controller-tests"
title = "Comprehensive tests for adaptive concurrency controller"
type = "task"
priority = 2
depends_on = ["controller-integration"]
scope = ["internal/nebula/nebula_test.go"]
+++

## Problem

The feedback controller has complex stateful behavior across multiple strategies. It needs thorough testing to ensure correctness and prevent regressions.

## Solution

Add table-driven tests covering controller behavior, strategy variants, and integration.

### Controller unit tests

| Scenario | Expected |
|----------|----------|
| New controller, ceiling=4, balanced → initial workers | 4 (starts at ceiling) |
| Clean wave, balanced → next workers | min(current+1, ceiling) |
| 1 conflict, balanced → next workers | max(1, current*0.5) |
| Multiple clean waves → ramps up to ceiling | reaches ceiling, stays there |
| Conflicted then clean → recovers | decreases then increases |
| Ceiling drops between waves → respects new ceiling | current capped |
| Never below 1 | even after many conflicts |
| Never above ceiling | even after many clean waves |

### Strategy-specific tests

| Strategy | Clean wave behavior | Conflict behavior |
|----------|-------------------|-------------------|
| speed | aggressive increase (+2) | gentle decrease (0.75) |
| cost | cautious increase (+1) | aggressive decrease (0.5) |
| quality | no increase | decrease on low satisfaction |
| balanced | moderate increase (+1) | moderate decrease (0.5) |

### Warm start tests

| Scenario | Expected |
|----------|----------|
| No history → uses initial workers | default start |
| History with final concurrency=3 → warm start | starts at 3 |
| History with conflicts → warm start conservative | starts below historical |

### Integration tests

| Scenario | Expected |
|----------|----------|
| WorkerGroup with nil Controller | static parallelism (Layer 1) |
| WorkerGroup with Controller, clean run | concurrency ramps up |
| WorkerGroup with Controller, conflicting run | concurrency reduces |
| WorkerGroup with Controller, nil Metrics | uses strategy defaults |

### Multi-wave simulation tests

Simulate a 5-wave execution with varying conflict patterns:
1. Wave 1: clean → increase
2. Wave 2: clean → increase
3. Wave 3: 2 conflicts → decrease
4. Wave 4: clean → increase
5. Wave 5: clean → increase

Verify the concurrency trajectory matches AIMD expectations.

## Files to Modify

- `internal/nebula/nebula_test.go` — Add controller and strategy tests

## Acceptance Criteria

- [ ] All four strategies tested
- [ ] AIMD trajectory verified across multi-wave simulation
- [ ] Warm start tested with and without history
- [ ] Integration tests cover nil/non-nil Controller
- [ ] Race detector clean: `go test -race ./internal/nebula/...`
- [ ] `go test ./internal/nebula/... -v` shows clear test names
