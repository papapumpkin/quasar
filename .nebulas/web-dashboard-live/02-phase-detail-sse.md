+++
id = "phase-detail-sse"
title = "Make phase detail page live with HTML-fragment SSE events"
type = "bug"
priority = 1
depends_on = ["accumulator-dashboard-data"]
+++

## Problem

The phase detail page (`phase_detail.html`) sets up `sse-connect="/events?phase={{.Phase.ID}}"` and `sse-swap="cycle-update"`, but:

1. **No `cycle-update` events are ever produced.** The SSE bridge only generates `phase-update`, `progress-update`, and `nebula-done` for the dashboard path.
2. **Phase-filtered SSE streams pass raw JSON**, not HTML. The SSE handler's `useBridge` is only true when `phaseFilter == ""` (dashboard). Phase detail pages get raw JSON events that htmx cannot swap.
3. The phase detail page is therefore completely static after initial render — no live cycle additions, no agent status updates, no cost updates.

## Solution

1. **Extend the SSE bridge** to handle phase-detail-scoped translations. Add methods that produce:
   - `cycle-update`: an HTML fragment for a new `<div class="cycle">` block, rendered from `CycleDetail` data. Sent on `phase.cycle.start` events.
   - `agent-update`: an HTML fragment updating an agent entry within a cycle. Sent on `phase.agent.start`, `phase.agent.done`, `phase.agent.output` events.
   - `phase-header-update`: an HTML fragment updating the phase header meta (status badge, cost, cycle count). Sent on status-changing events.

2. **Update the SSE handler** (`sse.go`) to use the bridge for phase-filtered streams too, not just the dashboard. The bridge should detect whether it's in dashboard mode or phase-detail mode based on the event types and produce appropriate fragments.

3. **Update `phase_detail.html`** to have proper `sse-swap` targets:
   - The `cycle-timeline` div should accept `cycle-update` events with `hx-swap="beforeend"` (append new cycles).
   - Each agent entry should have an ID like `agent-{phaseID}-{cycle}-{role}` for targeted OOB swaps.
   - The phase header meta section should have an ID for OOB status/cost updates.

4. **Add a phase-detail template partial** (`partials/cycle_block.html`) that renders a single cycle block, reusable for both initial render and SSE updates.

## Files

- `internal/web/sse_bridge.go` — add phase-detail translation methods
- `internal/web/sse.go` — enable bridge for phase-filtered streams
- `internal/web/templates/phase_detail.html` — add SSE swap targets and IDs
- `internal/web/templates/partials/cycle_block.html` — new partial for cycle rendering
- `internal/web/sse_bridge_test.go` — test phase-detail event translation

## Acceptance Criteria

- [ ] New cycles appear in the timeline in real time as `phase.cycle.start` events arrive
- [ ] Agent entries update (show "working..." then cost/duration) as `phase.agent.start` and `phase.agent.done` events arrive
- [ ] Phase header status badge and cost update in real time
- [ ] Agent output appears in the detail view when `phase.agent.output` events arrive
- [ ] Page still renders correctly on initial load with pre-existing accumulated data
- [ ] No regressions to dashboard SSE behavior
