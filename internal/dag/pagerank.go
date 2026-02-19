package dag

import "math"

// PageRankOptions configures the iterative PageRank algorithm.
type PageRankOptions struct {
	Damping       float64 // damping factor; typically 0.85
	Epsilon       float64 // convergence threshold
	MaxIterations int     // upper bound on iterations
}

// DefaultPageRankOptions returns production-ready defaults:
// damping 0.85, epsilon 1e-6, max 100 iterations.
func DefaultPageRankOptions() PageRankOptions {
	return PageRankOptions{
		Damping:       0.85,
		Epsilon:       1e-6,
		MaxIterations: 100,
	}
}

// PageRank computes iterative PageRank scores for all nodes in the DAG.
// Importance flows from dependents to their dependencies: a node depended
// on by many high-importance nodes receives a higher score.
//
// Dangling nodes (those with no dependencies) redistribute their rank
// uniformly across all nodes, following the standard PageRank treatment.
//
// Returns a map of node ID to raw PageRank score. Scores sum to
// approximately 1.0 across all nodes.
func (d *DAG) PageRank(opts PageRankOptions) map[string]float64 {
	n := len(d.nodes)
	if n == 0 {
		return make(map[string]float64)
	}

	nf := float64(n)
	initial := 1.0 / nf
	base := (1.0 - opts.Damping) / nf

	rank := make(map[string]float64, n)
	for id := range d.nodes {
		rank[id] = initial
	}

	for iter := 0; iter < opts.MaxIterations; iter++ {
		// Dangling node contribution: nodes with no dependencies
		// (out-degree 0 in the link graph) redistribute uniformly.
		var danglingSum float64
		for id := range d.nodes {
			if len(d.adjacency[id]) == 0 {
				danglingSum += rank[id]
			}
		}
		danglingShare := opts.Damping * danglingSum / nf

		newRank := make(map[string]float64, n)
		for v := range d.nodes {
			// Sum contributions from nodes that depend on v.
			var sum float64
			for u := range d.reverse[v] {
				outDeg := len(d.adjacency[u])
				if outDeg > 0 {
					sum += rank[u] / float64(outDeg)
				}
			}
			newRank[v] = base + opts.Damping*sum + danglingShare
		}

		// Convergence check: max absolute change across nodes.
		maxDelta := 0.0
		for id := range d.nodes {
			delta := math.Abs(newRank[id] - rank[id])
			if delta > maxDelta {
				maxDelta = delta
			}
		}

		rank = newRank
		if maxDelta < opts.Epsilon {
			break
		}
	}

	return rank
}
