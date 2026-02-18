+++
id = "union-find-tracks"
title = "Use Union-Find to partition DAG into independent parallel tracks"
type = "feature"
priority = 2
depends_on = ["dag-core"]
+++

## Problem

In a DAG with many tasks, some subsets are completely independent of each other — they share no dependencies or common ancestors. These independent subsets ("tracks") can be executed in parallel by different agents without any risk of conflict.

## Solution

Implement Union-Find (Disjoint Set Union) to efficiently partition the DAG:

### Union-Find Data Structure

```go
type UnionFind struct {
    parent map[string]string
    rank   map[string]int
}

func NewUnionFind() *UnionFind
func (uf *UnionFind) Find(x string) string    // path compression
func (uf *UnionFind) Union(x, y string)        // union by rank
func (uf *UnionFind) Connected(x, y string) bool
func (uf *UnionFind) Components() map[string][]string // group ID -> member IDs
```

### Track Partitioning

1. Initialize each node as its own set
2. For each edge (A depends on B), union A and B
3. Extract connected components — each component is an independent track
4. Assign `Node.TrackID` based on the component

### Track Properties

Each track has:
- A set of nodes that must be executed in topological order
- Complete independence from other tracks
- An aggregate impact score (sum or max of member impacts)
- An estimated cost/duration (sum of member estimates, if available)

Agents can claim entire tracks without worrying about file conflicts with agents on other tracks.

## Files

- `internal/dag/unionfind.go` — Union-Find implementation
- `internal/dag/tracks.go` — track partitioning and track metadata
- `internal/dag/tracks_test.go` — tests for partitioning various graph shapes

## Acceptance Criteria

- [ ] Union-Find correctly identifies connected components
- [ ] Independent subgraphs become separate tracks
- [ ] Single-chain graph produces one track
- [ ] Fully disconnected nodes each become their own track
- [ ] Track metadata (member list, aggregate impact) is correct
- [ ] `go test ./internal/dag/...` passes
