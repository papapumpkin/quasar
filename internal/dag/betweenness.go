package dag

// BetweennessCentrality computes normalized betweenness centrality for all
// nodes using Brandes' algorithm. Edges follow execution order (from a
// dependency to its dependents), so a node that sits on many paths from
// early tasks to late tasks receives a high score.
//
// Scores are normalized to [0, 1] using the directed-graph normalization
// factor (n-1)*(n-2). Returns a map of node ID to centrality score.
func (d *DAG) BetweennessCentrality() map[string]float64 {
	cb := make(map[string]float64, len(d.nodes))
	for id := range d.nodes {
		cb[id] = 0
	}

	n := len(d.nodes)
	if n < 3 {
		return cb
	}

	for s := range d.nodes {
		stack, sigma, pred := d.brandesBFS(s)
		d.brandesAccumulate(s, stack, sigma, pred, cb)
	}

	// Normalize to [0, 1] using directed-graph factor.
	normFactor := float64((n - 1) * (n - 2))
	for id := range cb {
		cb[id] /= normFactor
	}

	return cb
}

// brandesBFS performs the BFS phase of Brandes' algorithm from source s.
// It returns the visit stack (reverse BFS order for back-propagation),
// shortest-path counts (sigma), and predecessor lists (pred).
func (d *DAG) brandesBFS(s string) ([]string, map[string]float64, map[string][]string) {
	n := len(d.nodes)
	stack := make([]string, 0, n)
	pred := make(map[string][]string, n)
	sigma := make(map[string]float64, n)
	dist := make(map[string]int, n)

	for id := range d.nodes {
		dist[id] = -1
	}
	sigma[s] = 1
	dist[s] = 0

	queue := []string{s}
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		stack = append(stack, v)

		// Traverse execution-order edges: from v to its dependents.
		for w := range d.reverse[v] {
			if dist[w] < 0 {
				dist[w] = dist[v] + 1
				queue = append(queue, w)
			}
			if dist[w] == dist[v]+1 {
				sigma[w] += sigma[v]
				pred[w] = append(pred[w], v)
			}
		}
	}

	return stack, sigma, pred
}

// brandesAccumulate performs the back-propagation phase of Brandes'
// algorithm, accumulating pair-dependency values into the centrality map.
func (d *DAG) brandesAccumulate(s string, stack []string, sigma map[string]float64, pred map[string][]string, cb map[string]float64) {
	delta := make(map[string]float64, len(d.nodes))

	for i := len(stack) - 1; i >= 0; i-- {
		w := stack[i]
		for _, v := range pred[w] {
			delta[v] += (sigma[v] / sigma[w]) * (1 + delta[w])
		}
		if w != s {
			cb[w] += delta[w]
		}
	}
}
