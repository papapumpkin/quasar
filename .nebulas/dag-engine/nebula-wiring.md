+++
id = "nebula-wiring"
title = "Wire DAG engine into nebula orchestrator"
type = "feature"
priority = 3
depends_on = ["facade-strategy"]
+++

## Problem

The DAG engine is built but the nebula orchestrator still uses the old graph code. The analyzer needs to be wired in so that nebula execution benefits from impact scoring and track-based parallelism.

## Solution

1. **Replace graph usage in apply.go**:
   - Build a `TaskAnalyzer` from the nebula's phases and dependencies
   - Use `ReadyTasks()` for scheduling instead of manual adjacency list traversal
   - Use `Tracks()` to identify independent parallel groups

2. **Impact-aware scheduling**:
   - High-impact phases (bottlenecks) get scheduled first when workers are available
   - Critical path phases get priority over leaf nodes

3. **Track-based agent assignment**:
   - When `max_workers > 1`, assign workers to independent tracks
   - Workers on the same track execute sequentially (respecting topo order)
   - Workers on different tracks execute in parallel

4. **TUI integration**:
   - Show track assignments in the phase tree view
   - Show impact scores in the detail panel
   - Optionally show the critical path highlighted

## Files

- `internal/nebula/apply.go` — replace graph usage with TaskAnalyzer
- `internal/nebula/parallelism.go` — use tracks for parallel scheduling
- `internal/nebula/worker.go` — track-aware worker assignment
- `internal/tui/nebulaview.go` — optional: show track/impact info

## Acceptance Criteria

- [ ] Nebula execution uses TaskAnalyzer for scheduling
- [ ] Impact scoring influences phase priority
- [ ] Independent tracks are parallelized when max_workers > 1
- [ ] No regression in single-worker execution
- [ ] `go test ./internal/nebula/...` passes
- [ ] `go test ./internal/dag/...` passes
