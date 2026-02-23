+++
id = "expose-dag-caps"
title = "Export HasPath and add Connected/ComputeWaves/DepsFor to dag.DAG"
priority = 1
scope = ["internal/dag/**"]
+++

## Problem

`dag.DAG.hasPath` is unexported, and `dag.DAG` lacks `Connected`, `ComputeWaves`, and a method to retrieve direct dependencies for a node. The nebula package currently reimplements all of these on its own `nebula.Graph` type. Before we can remove `nebula.Graph`, the canonical `dag.DAG` must expose equivalent capabilities.

## Solution

Add the following methods to `dag.DAG`:

1. **Export `hasPath` as `HasPath(src, dst string) bool`** — rename the existing unexported method.
2. **Add `Connected(a, b string) bool`** — returns `HasPath(a,b) || HasPath(b,a)`.
3. **Add `ComputeWaves() ([]Wave, error)`** — layer-based Kahn's algorithm producing batches of nodes whose dependencies all fall in prior waves. The `Wave` type (`Number int`, `NodeIDs []string`) can live in `dag/dag.go`. Sort node IDs within each wave alphabetically for deterministic output.
4. **Add `DepsFor(id string) []string`** — returns direct dependency IDs for a node (the adjacency set), sorted alphabetically. This replaces the direct `graph.adjacency[id]` access in `tracker.go`, `dashboard.go`, and `validate.go`.
5. **Add `AddNodeIdempotent(id string, priority int)`** — idempotent variant of `AddNode` (no-op if node exists). This replaces `nebula.Graph.AddNode`'s no-op-on-duplicate behavior needed by hot-reload.
6. **Add `RemoveEdge(from, to string)`** — removes a single edge. `dag.DAG` currently only has `Remove` (node removal). Hot-reload needs edge removal for rollback.

Update `dag.DAG.AddEdge`'s internal cycle check to call the now-public `HasPath`.

Add tests for all new methods in `internal/dag/dag_test.go`.

## Files

- `internal/dag/dag.go` — add/export methods and `Wave` type
- `internal/dag/dag_test.go` — add tests for `HasPath`, `Connected`, `ComputeWaves`, `DepsFor`, `AddNodeIdempotent`, `RemoveEdge`

## Acceptance Criteria

- [ ] `HasPath` is exported and existing `AddEdge` cycle check still works
- [ ] `Connected(a,b)` returns true iff either direction has a path
- [ ] `ComputeWaves` produces correct wave groupings; returns `ErrCycle` on cycles
- [ ] `DepsFor` returns sorted direct dependencies for a node
- [ ] `AddNodeIdempotent` is a no-op for existing nodes
- [ ] `RemoveEdge` removes a single directed edge without affecting nodes
- [ ] All existing `dag` tests pass; new tests cover edge cases