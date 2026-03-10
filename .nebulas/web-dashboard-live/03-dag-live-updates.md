+++
id = "dag-live-updates"
title = "DAG page nodes update status colors via SSE"
type = "bug"
priority = 2
depends_on = ["accumulator-dashboard-data"]
+++

## Problem

The DAG page (`dag.html`) sets up `sse-connect="/events"` and `sse-swap="dag-update"` with `hx-swap="none"`, expecting OOB swaps on individual DAG nodes. However:

1. No code produces events of type `"dag-update"`.
2. The SSE bridge only translates `phase.*` events into `phase-update` (table row) and `nebula.progress` into `progress-update` (status bar). There's no DAG-specific translation.
3. DAG node status colors are set at initial render from `nebula.State` and never change.

## Solution

1. **Add a DAG translation path to the SSE bridge.** When a `phase.*` status-changing event arrives (e.g., `phase.task.started`, `phase.task.complete`), produce a `dag-update` event containing OOB-swapped SVG `<rect>` and edge elements with updated CSS classes.

2. **Approach**: Rather than re-rendering individual SVG elements (which is fragile), re-render the entire SVG content and send it as a single OOB swap targeting the `.dag-container` div. This is simpler and handles edge color updates too (edges turn green when dependencies are satisfied).

3. **Update `dag.html`** to give the SVG container a swappable ID (e.g., `id="dag-svg-container"`).

4. **Add a `renderDAGFragment` method** to SSEBridge that calls `ComputeDAGLayout` with current state and renders the SVG portion of the DAG template.

5. The SSE handler already broadcasts all events to dashboard-mode clients. The bridge just needs to recognize phase status events and add DAG fragment generation alongside the existing phase-row generation.

## Files

- `internal/web/sse_bridge.go` — add `translatePhaseEventForDAG` or augment `translatePhaseEvent` to also emit a `dag-update` event
- `internal/web/templates/dag.html` — add swappable container ID
- `internal/web/templates/partials/dag_svg.html` — optional: extract SVG into a partial for reuse
- `internal/web/handler_dag.go` — no changes needed if partial approach is used

## Acceptance Criteria

- [ ] DAG node colors update in real time when phases start, complete, or fail
- [ ] DAG edge colors update (satisfied edges turn green) as dependencies resolve
- [ ] Initial render still works correctly
- [ ] No layout jumps or SVG sizing issues on update
