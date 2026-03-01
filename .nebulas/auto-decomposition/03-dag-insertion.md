+++
id = "dag-insertion"
title = "Implement DAG surgery to replace a struggling phase with sub-phases"
type = "feature"
priority = 1
depends_on = ["decomposition-architect"]
+++

## Problem

After the architect produces 2-3 sub-phases, they must be inserted into the live `*dag.DAG` and the nebula's phase registry in a way that preserves dependency correctness. The original phase must be removed, its incoming dependencies rewired to the sub-phases, and its outgoing dependents rewired to depend on all sub-phases (so they wait for the full decomposition to complete). This is a graph surgery operation that must be atomic with respect to the `WorkerGroup` scheduler.

The existing `*dag.DAG` has `AddNode`, `AddEdge`, `Remove`, and `Descendants` methods. The `HotReloader` in `internal/nebula/hotreload.go` already performs similar graph mutations for file-system-driven hot adds. This phase builds a dedicated decomposition graph operation that the worker integration (phase 04) will invoke.

## Solution

Add a new file `internal/nebula/decompose_dag.go` with the graph surgery logic.

### Types

```go
// DecomposeOp describes a decomposition operation to be applied to the DAG.
type DecomposeOp struct {
    OriginalPhaseID string
    SubPhases       []SubPhaseEntry
}

// SubPhaseEntry holds the information needed to insert a sub-phase into the DAG.
type SubPhaseEntry struct {
    Spec     PhaseSpec        // parsed phase spec from the architect
    Body     string           // markdown body for the phase file
    Filename string           // filename for the phase file (e.g., "03a-original-id-part-1.md")
}
```

### Core Function

```go
// ApplyDecomposition performs atomic graph surgery on the DAG:
//  1. Records the original phase's predecessors (DepsFor) and successors (Descendants).
//  2. Removes the original phase node from the DAG.
//  3. Adds each sub-phase as a new node.
//  4. Wires each sub-phase to depend on the original phase's predecessors.
//  5. Wires the original phase's successors to depend on ALL sub-phases.
//  6. Wires inter-sub-phase dependencies declared in SubPhaseEntry.Spec.DependsOn.
//  7. Validates no cycles were introduced (DAG.AddEdge returns dag.ErrCycle).
//
// The function returns the list of sub-phase IDs that were added.
// On error, partial mutations may have occurred — the caller should treat the DAG as corrupt.
func ApplyDecomposition(d *dag.DAG, op DecomposeOp) ([]string, error)
```

**Dependency rewiring logic**:

Given original phase P with predecessors {A, B} and successors {X, Y}:
- Sub-phases S1, S2 are added.
- Edges A->S1, A->S2, B->S1, B->S2 are created (sub-phases inherit P's prerequisites).
- Edges S1->X, S1->Y, S2->X, S2->Y are created (P's dependents now wait on all sub-phases).
- If S2 declares `depends_on = ["S1"]`, edge S1->S2 is added.
- P is removed (which also removes all edges incident to P).

The order matters: remove P first to free the node ID, then add sub-phases and edges.

### Phase Registry Update

```go
// ApplyDecompositionToNebula updates the Nebula's in-memory phase registry and writes
// the sub-phase files to disk. It calls ApplyDecomposition for the DAG mutation and
// then updates Nebula.PhasesByID and Nebula.Phases to reflect the new sub-phases.
// It also removes the original phase's entry from PhasesByID.
func ApplyDecompositionToNebula(neb *Nebula, d *dag.DAG, op DecomposeOp) ([]string, error)
```

This function:
1. Calls `ApplyDecomposition(d, op)` for graph surgery.
2. Writes each sub-phase to `neb.Dir` as a markdown file using the existing phase file format (`+++` frontmatter + body).
3. Adds each sub-phase to `neb.PhasesByID` and appends to `neb.Phases`.
4. Deletes the original phase from `neb.PhasesByID` (the file on disk is left intact but annotated with a `decomposed = true` frontmatter flag for traceability).

### Fabric State Transition

When decomposition occurs, the original phase should transition to a new fabric state. Add a constant:

```go
// In internal/fabric/fabric.go, alongside existing state constants:
StateDecomposed = "decomposed"
```

The caller (worker integration, phase 04) is responsible for calling `Fabric.SetPhaseState(ctx, originalPhaseID, fabric.StateDecomposed)`.

## Files

- `internal/nebula/decompose_dag.go` — `DecomposeOp`, `SubPhaseEntry`, `ApplyDecomposition`, `ApplyDecompositionToNebula`
- `internal/nebula/decompose_dag_test.go` — table-driven tests: simple 2-sub decomposition, 3-sub with inter-dependencies, successor rewiring, cycle detection on conflicting edges, empty predecessors, empty successors, duplicate sub-phase ID error
- `internal/fabric/fabric.go` — add `StateDecomposed` constant

## Acceptance Criteria

- [ ] `ApplyDecomposition` removes the original phase node from the DAG
- [ ] Sub-phase nodes are added with correct `dag.Node` entries (ID, Priority from spec)
- [ ] Predecessor edges are rewired: each sub-phase depends on all original predecessors
- [ ] Successor edges are rewired: each original successor depends on all sub-phases
- [ ] Inter-sub-phase `depends_on` edges are honored
- [ ] `ApplyDecomposition` returns `dag.ErrCycle` if the new edges would create a cycle
- [ ] `ApplyDecompositionToNebula` writes valid `+++`-delimited phase files to the nebula directory
- [ ] `ApplyDecompositionToNebula` updates `PhasesByID` and removes the original phase entry
- [ ] `StateDecomposed` constant is added to `internal/fabric/fabric.go`
- [ ] `go test ./internal/nebula/...` passes with at least 8 DAG surgery test cases
- [ ] `go vet ./...` reports no issues
