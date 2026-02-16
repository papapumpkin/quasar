+++
id = "graph-has-path"
title = "Add transitive dependency check to Graph"
type = "feature"
priority = 1
depends_on = []
scope = ["internal/nebula/graph.go"]
+++

## Problem

Scope overlap validation needs to know whether two phases are transitively connected via dependencies (if they are, they're serialized and overlap is safe). The existing `Graph` only has `Sort()` and `Ready()` — no path-existence check.

## Solution

Add a `HasPath(from, to string) bool` method to `Graph` that performs BFS on the adjacency map to check if `from` transitively depends on `to`.

```go
// HasPath reports whether there is a directed path from 'from' to 'to'
// in the dependency graph (i.e., 'from' transitively depends on 'to').
func (g *Graph) HasPath(from, to string) bool {
    // BFS on g.adjacency starting from 'from', looking for 'to'
}
```

Also add a convenience method:

```go
// Connected reports whether from and to are connected in either direction.
func (g *Graph) Connected(a, b string) bool {
    return g.HasPath(a, b) || g.HasPath(b, a)
}
```

## Files to Modify

- `internal/nebula/graph.go` — Add HasPath and Connected methods

## Acceptance Criteria

- [ ] `HasPath("a", "b")` returns true when a depends_on b
- [ ] `HasPath("a", "c")` returns true when a→b→c (transitive)
- [ ] `HasPath("a", "b")` returns false when no path exists
- [ ] `Connected("a", "b")` returns true regardless of direction
- [ ] `go test ./internal/nebula/...` passes
