+++
id = "dag-core"
title = "Implement the core DAG data structure with topological sort"
type = "feature"
priority = 1
depends_on = []
+++

## Problem

Quasar needs a robust DAG (Directed Acyclic Graph) engine to model task dependencies. The current `internal/nebula/graph.go` has basic graph operations but lacks the algorithmic depth needed for intelligent scheduling, impact analysis, and parallel track partitioning.

## Solution

Build a core DAG package with the fundamental data structure and topological sort:

### Data Structure

```go
// DAG represents a directed acyclic graph of tasks.
type DAG struct {
    nodes map[string]*Node
    edges map[string][]string // adjacency list: node -> dependencies
}

// Node represents a task in the DAG.
type Node struct {
    ID       string
    Priority int
    Metadata map[string]any
    // Computed fields (populated by analysis)
    Impact   float64 // composite score from PageRank + Betweenness
    TrackID  int     // Union-Find partition
}
```

### Topological Sort

Implement Kahn's algorithm for topological ordering:
- Returns a valid execution order respecting all dependencies
- Detects cycles and returns an error if the graph is not a DAG
- Supports priority-aware ordering: among nodes with no unmet dependencies, prefer higher-priority nodes

### Basic Operations

- `AddNode(id string, priority int)`
- `AddEdge(from, to string)` — "from" depends on "to"
- `Remove(id string)` — remove node and all associated edges
- `TopologicalSort() ([]string, error)`
- `Ready() []string` — nodes with all dependencies satisfied
- `Ancestors(id string) []string` — all transitive dependencies
- `Descendants(id string) []string` — all transitive dependents

## Files

- `internal/dag/dag.go` — core DAG structure and topological sort
- `internal/dag/dag_test.go` — tests with various graph topologies (linear, diamond, wide, etc.)

## Acceptance Criteria

- [ ] DAG supports add/remove nodes and edges
- [ ] Topological sort produces valid orderings
- [ ] Cycle detection returns clear error
- [ ] Priority-aware ready set computation
- [ ] `go test ./internal/dag/...` passes with multiple graph topologies
