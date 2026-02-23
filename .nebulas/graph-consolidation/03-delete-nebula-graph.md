+++
id = "delete-nebula-graph"
title = "Delete nebula/graph.go and its tests"
priority = 2
depends_on = ["migrate-consumers"]
scope = ["internal/nebula/graph.go", "internal/nebula/graph_test.go"]
+++

## Problem

After all consumers have been migrated to `dag.DAG`, `internal/nebula/graph.go` (and any associated test file) is dead code. Keeping it around risks someone accidentally importing the wrong graph type.

## Solution

1. Delete `internal/nebula/graph.go`.
2. Delete `internal/nebula/graph_test.go` if it exists.
3. Remove the `nebula.Wave` type if it was replaced by `dag.Wave` in phase 2. If it was aliased, the alias is fine to keep.
4. Verify no remaining imports or references.

## Files

- `internal/nebula/graph.go` — delete
- `internal/nebula/graph_test.go` — delete (if exists)

## Acceptance Criteria

- [ ] `internal/nebula/graph.go` no longer exists
- [ ] `go build ./...` succeeds with no references to deleted types
- [ ] `go test ./...` passes