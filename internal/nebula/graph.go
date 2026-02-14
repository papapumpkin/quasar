package nebula

import "fmt"

// Graph is a dependency DAG over task IDs.
type Graph struct {
	// adjacency maps taskID → set of task IDs it depends on.
	adjacency map[string]map[string]bool
	// reverse maps taskID → set of task IDs that depend on it.
	reverse map[string]map[string]bool
}

// NewGraph builds a dependency graph from a list of task specs.
func NewGraph(tasks []TaskSpec) *Graph {
	g := &Graph{
		adjacency: make(map[string]map[string]bool),
		reverse:   make(map[string]map[string]bool),
	}
	for _, t := range tasks {
		if g.adjacency[t.ID] == nil {
			g.adjacency[t.ID] = make(map[string]bool)
		}
		if g.reverse[t.ID] == nil {
			g.reverse[t.ID] = make(map[string]bool)
		}
		for _, dep := range t.DependsOn {
			g.adjacency[t.ID][dep] = true
			if g.reverse[dep] == nil {
				g.reverse[dep] = make(map[string]bool)
			}
			g.reverse[dep][t.ID] = true
		}
	}
	return g
}

// Sort returns task IDs in topological order (dependencies first).
// Returns an error if a cycle is detected.
func (g *Graph) Sort() ([]string, error) {
	// Kahn's algorithm.
	inDegree := make(map[string]int)
	for id := range g.adjacency {
		inDegree[id] = len(g.adjacency[id])
	}

	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	var sorted []string
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		sorted = append(sorted, id)

		for dependent := range g.reverse[id] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	if len(sorted) != len(g.adjacency) {
		return nil, fmt.Errorf("%w: not all tasks could be ordered", ErrDependencyCycle)
	}

	return sorted, nil
}

// Ready returns task IDs that have no unfinished dependencies.
// done is the set of task IDs that are already completed.
func (g *Graph) Ready(done map[string]bool) []string {
	var ready []string
	for id, deps := range g.adjacency {
		if done[id] {
			continue
		}
		allMet := true
		for dep := range deps {
			if !done[dep] {
				allMet = false
				break
			}
		}
		if allMet {
			ready = append(ready, id)
		}
	}
	return ready
}
