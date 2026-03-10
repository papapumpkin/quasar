+++
id = "accumulator-dashboard-data"
title = "Wire PhaseAccumulator data into dashboard initial render"
type = "bug"
priority = 1
depends_on = []
+++

## Problem

The dashboard's `buildDashboardData` and `renderPhaseRow` (SSE bridge) both hardcode cost as `0.0000` and cycles as `"0/max"`. The `PhaseAccumulator` correctly tracks per-phase cost and cycle counts from bus events, but neither the initial page render nor the SSE bridge reads from it.

Specifically:
- `handler_dashboard.go:140` — `CostUSD: fmt.Sprintf("%.4f", 0.0)` is hardcoded
- `handler_dashboard.go:131` — cycles is always `"0/" + max`
- `sse_bridge.go:162` — same hardcoded zeros in `renderPhaseRow`

The `PhaseAccumulator` has all the data (see `PhaseDetail.TotalCost`, `CycleDetail` list), but `buildDashboardData` doesn't consult it.

## Solution

1. Pass the `*PhaseAccumulator` into `buildDashboardData` (or make it a method on `Server`).
2. When building each `PhaseRow`, look up the accumulated `PhaseDetail` for that phase ID.
3. If found, use `detail.TotalCost` for cost and `len(detail.Cycles)` for the current cycle count.
4. In `renderPhaseRow` (SSE bridge), do the same — read from `s.accumulator.Get(phaseID)`.
5. Also read status from the accumulator when it has a more recent status than `nebula.State` (e.g., the accumulator knows "in_progress" before the state file is written).

## Files

- `internal/web/handler_dashboard.go` — update `buildDashboardData` to accept and read from accumulator
- `internal/web/sse_bridge.go` — update `renderPhaseRow` to read from `s.accumulator`

## Acceptance Criteria

- [ ] Dashboard initial render shows real cost values from accumulated bus events
- [ ] Dashboard initial render shows real cycle counts (e.g., "2/5" not "0/5")
- [ ] SSE-pushed phase row updates also show real cost and cycle data
- [ ] When no events have arrived yet, gracefully falls back to zero/pending defaults
- [ ] Existing tests in `handler_dashboard_test.go` still pass (update as needed)
