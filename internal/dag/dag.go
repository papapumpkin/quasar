// Package dag provides a directed acyclic graph engine for modeling task
// dependencies. It supports topological sorting, cycle detection,
// priority-aware scheduling, and transitive dependency queries.
package dag

import (
	"container/heap"
	"errors"
	"fmt"
	"sort"
)

// ErrCycle is returned when the graph contains a dependency cycle.
var ErrCycle = errors.New("cycle detected")

// ErrNodeNotFound is returned when an operation references a non-existent node.
var ErrNodeNotFound = errors.New("node not found")

// ErrDuplicateNode is returned when adding a node that already exists.
var ErrDuplicateNode = errors.New("duplicate node")

// ErrSelfEdge is returned when an edge would create a self-loop.
var ErrSelfEdge = errors.New("self-referencing edge")

// Wave represents a batch of nodes whose dependencies all fall in prior waves.
type Wave struct {
	Number  int      // zero-based wave index
	NodeIDs []string // node IDs in this wave, sorted alphabetically
}

// Node represents a task in the DAG.
type Node struct {
	ID       string
	Priority int            // higher value = higher priority
	Metadata map[string]any // arbitrary key-value data

	// Computed fields populated by analysis passes.
	Impact  float64 // composite score from PageRank + Betweenness
	TrackID int     // Union-Find partition identifier
}

// DAG represents a directed acyclic graph of tasks.
// Edges point from a node to its dependencies: if A depends on B,
// there is an edge from A to B.
type DAG struct {
	nodes map[string]*Node
	// adjacency maps nodeID → set of dependency IDs (forward edges).
	adjacency map[string]map[string]bool
	// reverse maps nodeID → set of dependent IDs (backward edges).
	reverse map[string]map[string]bool
}

// New creates an empty DAG.
func New() *DAG {
	return &DAG{
		nodes:     make(map[string]*Node),
		adjacency: make(map[string]map[string]bool),
		reverse:   make(map[string]map[string]bool),
	}
}

// AddNode adds a node with the given ID and priority. Returns
// ErrDuplicateNode if a node with that ID already exists.
func (d *DAG) AddNode(id string, priority int) error {
	if _, exists := d.nodes[id]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateNode, id)
	}
	d.nodes[id] = &Node{
		ID:       id,
		Priority: priority,
		Metadata: make(map[string]any),
	}
	d.adjacency[id] = make(map[string]bool)
	d.reverse[id] = make(map[string]bool)
	return nil
}

// AddEdge adds a dependency edge: from depends on to. Both nodes must
// already exist. Returns an error if either node is missing, the edge
// would create a self-loop, or the edge would introduce a cycle.
func (d *DAG) AddEdge(from, to string) error {
	if from == to {
		return fmt.Errorf("%w: %s", ErrSelfEdge, from)
	}
	if _, ok := d.nodes[from]; !ok {
		return fmt.Errorf("%w: %s", ErrNodeNotFound, from)
	}
	if _, ok := d.nodes[to]; !ok {
		return fmt.Errorf("%w: %s", ErrNodeNotFound, to)
	}
	// Skip if edge already exists.
	if d.adjacency[from][to] {
		return nil
	}
	// Check if adding this edge would create a cycle: does 'from' already
	// have a path reachable from 'to'? If so, adding to→...→from + from→to
	// would create a cycle.
	if d.HasPath(to, from) {
		return fmt.Errorf("%w: edge %s → %s would create a cycle", ErrCycle, from, to)
	}
	d.adjacency[from][to] = true
	d.reverse[to][from] = true
	return nil
}

// Remove removes a node and all its associated edges from the DAG.
// Returns ErrNodeNotFound if the node does not exist.
func (d *DAG) Remove(id string) error {
	if _, ok := d.nodes[id]; !ok {
		return fmt.Errorf("%w: %s", ErrNodeNotFound, id)
	}
	// Remove forward edges (this node's dependencies).
	for dep := range d.adjacency[id] {
		delete(d.reverse[dep], id)
	}
	delete(d.adjacency, id)

	// Remove reverse edges (nodes that depend on this node).
	for dependent := range d.reverse[id] {
		delete(d.adjacency[dependent], id)
	}
	delete(d.reverse, id)

	delete(d.nodes, id)
	return nil
}

// AddNodeIdempotent adds a node with the given ID and priority. If a node
// with that ID already exists, it is a no-op (the existing node is unchanged).
func (d *DAG) AddNodeIdempotent(id string, priority int) {
	if _, exists := d.nodes[id]; exists {
		return
	}
	d.nodes[id] = &Node{
		ID:       id,
		Priority: priority,
		Metadata: make(map[string]any),
	}
	d.adjacency[id] = make(map[string]bool)
	d.reverse[id] = make(map[string]bool)
}

// RemoveEdge removes a single directed edge from → to. If either node does
// not exist or the edge does not exist, it is a no-op.
func (d *DAG) RemoveEdge(from, to string) {
	if d.adjacency[from] != nil {
		delete(d.adjacency[from], to)
	}
	if d.reverse[to] != nil {
		delete(d.reverse[to], from)
	}
}

// Connected reports whether there is a directed path between a and b in
// either direction: HasPath(a,b) || HasPath(b,a).
func (d *DAG) Connected(a, b string) bool {
	return d.HasPath(a, b) || d.HasPath(b, a)
}

// DepsFor returns the direct dependency IDs for the given node, sorted
// alphabetically. Returns nil if the node does not exist or has no
// dependencies.
func (d *DAG) DepsFor(id string) []string {
	adj, ok := d.adjacency[id]
	if !ok || len(adj) == 0 {
		return nil
	}
	deps := make([]string, 0, len(adj))
	for dep := range adj {
		deps = append(deps, dep)
	}
	sort.Strings(deps)
	return deps
}

// ComputeWaves produces layer-based wave groupings using Kahn's algorithm.
// Each wave contains nodes whose dependencies all fall in prior waves.
// Node IDs within each wave are sorted alphabetically for deterministic output.
// Returns ErrCycle if the graph contains a cycle.
func (d *DAG) ComputeWaves() ([]Wave, error) {
	if len(d.nodes) == 0 {
		return nil, nil
	}

	// Compute in-degree (number of forward edges / dependencies) for each node.
	inDegree := make(map[string]int, len(d.nodes))
	for id := range d.nodes {
		inDegree[id] = len(d.adjacency[id])
	}

	// Seed with zero-dependency nodes.
	var current []string
	for id, deg := range inDegree {
		if deg == 0 {
			current = append(current, id)
		}
	}
	sort.Strings(current)

	var waves []Wave
	processed := 0

	for len(current) > 0 {
		wave := Wave{
			Number:  len(waves),
			NodeIDs: current,
		}
		waves = append(waves, wave)
		processed += len(current)

		var next []string
		for _, id := range current {
			// For each dependent of this node, decrement in-degree.
			for dependent := range d.reverse[id] {
				inDegree[dependent]--
				if inDegree[dependent] == 0 {
					next = append(next, dependent)
				}
			}
		}
		sort.Strings(next)
		current = next
	}

	if processed != len(d.nodes) {
		return nil, fmt.Errorf("%w: not all nodes could be grouped into waves (%d of %d)",
			ErrCycle, processed, len(d.nodes))
	}
	return waves, nil
}

// Node returns the node with the given ID, or nil if not found.
func (d *DAG) Node(id string) *Node {
	return d.nodes[id]
}

// Nodes returns all node IDs in the DAG, sorted alphabetically.
func (d *DAG) Nodes() []string {
	ids := make([]string, 0, len(d.nodes))
	for id := range d.nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Len returns the number of nodes in the DAG.
func (d *DAG) Len() int {
	return len(d.nodes)
}

// TopologicalSort returns node IDs in a valid topological order
// (dependencies come before dependents). Among nodes with all
// dependencies satisfied, higher-priority nodes are emitted first.
// Returns ErrCycle if the graph contains a cycle.
func (d *DAG) TopologicalSort() ([]string, error) {
	inDegree := make(map[string]int, len(d.nodes))
	for id := range d.nodes {
		inDegree[id] = len(d.adjacency[id])
	}

	// Seed priority queue with zero-dependency nodes.
	pq := &nodeHeap{nodes: d.nodes}
	for _, id := range d.zeroDegreeNodes(inDegree) {
		heap.Push(pq, id)
	}

	sorted := make([]string, 0, len(d.nodes))
	for pq.Len() > 0 {
		id := heap.Pop(pq).(string)
		sorted = append(sorted, id)

		// Push newly freed dependents into the priority queue.
		for dependent := range d.reverse[id] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				heap.Push(pq, dependent)
			}
		}
	}

	if len(sorted) != len(d.nodes) {
		return nil, fmt.Errorf("%w: not all nodes could be ordered (%d of %d)",
			ErrCycle, len(sorted), len(d.nodes))
	}
	return sorted, nil
}

// Ready returns node IDs that have all dependencies satisfied, given
// a set of completed node IDs. Results are sorted by priority descending
// (highest priority first), with alphabetical tie-breaking.
func (d *DAG) Ready(done map[string]bool) []string {
	var ready []string
	for id := range d.nodes {
		if done[id] {
			continue
		}
		allMet := true
		for dep := range d.adjacency[id] {
			if !done[dep] {
				allMet = false
				break
			}
		}
		if allMet {
			ready = append(ready, id)
		}
	}
	return d.prioritySorted(ready)
}

// Ancestors returns all transitive dependencies of the given node
// (i.e., everything it transitively depends on). The result is sorted
// alphabetically. Returns nil if the node has no dependencies or does
// not exist.
func (d *DAG) Ancestors(id string) []string {
	if _, ok := d.nodes[id]; !ok {
		return nil
	}
	visited := make(map[string]bool)
	d.collectAncestors(id, visited)
	result := make([]string, 0, len(visited))
	for v := range visited {
		result = append(result, v)
	}
	sort.Strings(result)
	return result
}

// Descendants returns all transitive dependents of the given node
// (i.e., everything that transitively depends on it). The result is
// sorted alphabetically. Returns nil if the node has no dependents or
// does not exist.
func (d *DAG) Descendants(id string) []string {
	if _, ok := d.nodes[id]; !ok {
		return nil
	}
	visited := make(map[string]bool)
	d.collectDescendants(id, visited)
	result := make([]string, 0, len(visited))
	for v := range visited {
		result = append(result, v)
	}
	sort.Strings(result)
	return result
}

// HasPath reports whether there is a directed path from src to dst
// through the dependency graph (forward edges).
func (d *DAG) HasPath(src, dst string) bool {
	if src == dst {
		return false
	}
	visited := make(map[string]bool)
	queue := []string{src}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for dep := range d.adjacency[cur] {
			if dep == dst {
				return true
			}
			if !visited[dep] {
				visited[dep] = true
				queue = append(queue, dep)
			}
		}
	}
	return false
}

// collectAncestors performs an iterative BFS over forward edges from id,
// collecting all reachable nodes (transitive dependencies).
func (d *DAG) collectAncestors(id string, visited map[string]bool) {
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for dep := range d.adjacency[cur] {
			if !visited[dep] {
				visited[dep] = true
				queue = append(queue, dep)
			}
		}
	}
}

// collectDescendants performs an iterative BFS over reverse edges from id,
// collecting all reachable nodes (transitive dependents).
func (d *DAG) collectDescendants(id string, visited map[string]bool) {
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for dep := range d.reverse[cur] {
			if !visited[dep] {
				visited[dep] = true
				queue = append(queue, dep)
			}
		}
	}
}

// zeroDegreeNodes returns IDs from the in-degree map that have zero value.
func (d *DAG) zeroDegreeNodes(inDegree map[string]int) []string {
	var result []string
	for id, deg := range inDegree {
		if deg == 0 {
			result = append(result, id)
		}
	}
	return result
}

// prioritySorted returns a copy of ids sorted by node priority descending,
// with alphabetical ID as tiebreaker.
func (d *DAG) prioritySorted(ids []string) []string {
	if len(ids) <= 1 {
		return ids
	}
	sorted := make([]string, len(ids))
	copy(sorted, ids)
	sort.Slice(sorted, func(i, j int) bool {
		pi := d.nodes[sorted[i]].Priority
		pj := d.nodes[sorted[j]].Priority
		if pi != pj {
			return pi > pj // higher priority first
		}
		return sorted[i] < sorted[j] // alphabetical tiebreaker
	})
	return sorted
}

// nodeHeap implements container/heap.Interface for priority-ordered node IDs.
// Nodes with higher priority are popped first; alphabetical ID breaks ties.
type nodeHeap struct {
	ids   []string
	nodes map[string]*Node
}

func (h *nodeHeap) Len() int { return len(h.ids) }

func (h *nodeHeap) Less(i, j int) bool {
	pi := h.nodes[h.ids[i]].Priority
	pj := h.nodes[h.ids[j]].Priority
	if pi != pj {
		return pi > pj // higher priority first (max-heap)
	}
	return h.ids[i] < h.ids[j] // alphabetical tiebreaker
}

func (h *nodeHeap) Swap(i, j int) { h.ids[i], h.ids[j] = h.ids[j], h.ids[i] }

func (h *nodeHeap) Push(x any) { h.ids = append(h.ids, x.(string)) }

func (h *nodeHeap) Pop() any {
	old := h.ids
	n := len(old)
	id := old[n-1]
	h.ids = old[:n-1]
	return id
}
