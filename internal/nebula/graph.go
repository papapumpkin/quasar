package nebula

import (
	"fmt"
	"sort"
)

// Graph is a dependency DAG over phase IDs.
type Graph struct {
	// adjacency maps phaseID → set of phase IDs it depends on.
	adjacency map[string]map[string]bool
	// reverse maps phaseID → set of phase IDs that depend on it.
	reverse map[string]map[string]bool
}

// NewGraph builds a dependency graph from a list of phase specs.
func NewGraph(phases []PhaseSpec) *Graph {
	g := &Graph{
		adjacency: make(map[string]map[string]bool),
		reverse:   make(map[string]map[string]bool),
	}
	for _, p := range phases {
		if g.adjacency[p.ID] == nil {
			g.adjacency[p.ID] = make(map[string]bool)
		}
		if g.reverse[p.ID] == nil {
			g.reverse[p.ID] = make(map[string]bool)
		}
		for _, dep := range p.DependsOn {
			g.adjacency[p.ID][dep] = true
			if g.reverse[dep] == nil {
				g.reverse[dep] = make(map[string]bool)
			}
			g.reverse[dep][p.ID] = true
		}
	}
	return g
}

// Sort returns phase IDs in topological order (dependencies first).
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
		return nil, fmt.Errorf("%w: not all phases could be ordered", ErrDependencyCycle)
	}

	return sorted, nil
}

// Ready returns phase IDs that have no unfinished dependencies.
// done is the set of phase IDs that are already completed.
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

// Wave represents a group of phases that can execute in parallel
// because all their dependencies are satisfied by prior waves.
type Wave struct {
	Number   int      // 1-based wave number
	PhaseIDs []string // Phase IDs in this wave
}

// ComputeWaves groups phase IDs into dependency waves using Kahn's algorithm.
// Wave 1 contains phases with no dependencies, wave 2 contains phases whose
// dependencies are all in wave 1, and so on. Returns an error if a cycle is detected.
func (g *Graph) ComputeWaves() ([]Wave, error) {
	inDegree := make(map[string]int)
	for id := range g.adjacency {
		inDegree[id] = len(g.adjacency[id])
	}

	// Collect initial wave: nodes with zero in-degree.
	var current []string
	for id, deg := range inDegree {
		if deg == 0 {
			current = append(current, id)
		}
	}

	var waves []Wave
	visited := 0
	for len(current) > 0 {
		sort.Strings(current) // deterministic ordering within each wave
		waves = append(waves, Wave{
			Number:   len(waves) + 1,
			PhaseIDs: current,
		})
		visited += len(current)

		var next []string
		for _, id := range current {
			for dependent := range g.reverse[id] {
				inDegree[dependent]--
				if inDegree[dependent] == 0 {
					next = append(next, dependent)
				}
			}
		}
		current = next
	}

	if visited != len(g.adjacency) {
		return nil, fmt.Errorf("%w: not all phases could be grouped into waves", ErrDependencyCycle)
	}

	return waves, nil
}
