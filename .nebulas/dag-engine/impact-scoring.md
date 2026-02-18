+++
id = "impact-scoring"
title = "Implement PageRank + Betweenness Centrality for impact scoring"
type = "feature"
priority = 2
depends_on = ["dag-core"]
+++

## Problem

Not all tasks in a DAG are equally important. Tasks that many others depend on (high centrality) or that sit on critical paths (high betweenness) should be prioritized and given more attention during review. We need a composite impact score for each node.

## Solution

Implement two graph algorithms and combine their scores:

### PageRank (Influence Score)

Adapted for DAGs — measures how "important" a node is based on what depends on it:
- A node depended on by many high-importance nodes gets a high score
- Uses the standard iterative PageRank with damping factor (0.85)
- Converge after score changes fall below epsilon (1e-6) or max iterations (100)
- In a DAG context, this naturally flows importance from leaves to roots

### Betweenness Centrality (Bottleneck Score)

Measures how often a node appears on shortest paths between other node pairs:
- High betweenness = bottleneck that blocks many downstream tasks
- Use Brandes' algorithm for efficient computation: O(V * E)
- Normalize to [0, 1] range

### Composite Impact Score

Combine both into a single score per node:
```
Impact = alpha * PageRank + (1-alpha) * Betweenness
```
Where alpha is configurable (default 0.6 — slightly favor influence over bottleneck).

Store the computed impact on each `Node.Impact` field.

## Files

- `internal/dag/pagerank.go` — PageRank implementation
- `internal/dag/betweenness.go` — Betweenness Centrality (Brandes' algorithm)
- `internal/dag/scoring.go` — composite impact scoring
- `internal/dag/scoring_test.go` — tests with known graph fixtures and expected scores

## Acceptance Criteria

- [ ] PageRank converges for DAGs of various sizes
- [ ] Betweenness correctly identifies bottleneck nodes
- [ ] Composite score combines both with configurable weight
- [ ] Known test fixtures produce expected relative orderings
- [ ] `go test ./internal/dag/...` passes
