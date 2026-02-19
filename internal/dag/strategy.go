package dag

import (
	"fmt"
	"sort"
	"strings"
)

// ReportStrategy defines how to present DAG analysis results.
// Each implementation produces a distinct view of the same underlying
// graph state, allowing consumers to choose the output format that
// best suits their needs.
type ReportStrategy interface {
	Render(dag *DAG, tracks []Track) string
}

// ExecutionPlanStrategy renders an ordered task list showing each task
// with its dependencies. Tasks appear in topological order so the
// output doubles as a step-by-step execution plan.
type ExecutionPlanStrategy struct{}

// Render produces a numbered execution plan with dependency annotations.
func (s ExecutionPlanStrategy) Render(d *DAG, _ []Track) string {
	order, err := d.TopologicalSort()
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	if len(order) == 0 {
		return "No tasks in graph."
	}

	var b strings.Builder
	b.WriteString("# Execution Plan\n\n")
	for i, id := range order {
		node := d.Node(id)
		fmt.Fprintf(&b, "%d. %s (priority: %d)", i+1, id, node.Priority)

		deps := sortedDeps(d, id)
		if len(deps) > 0 {
			fmt.Fprintf(&b, " [depends on: %s]", strings.Join(deps, ", "))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// ImpactReportStrategy renders tasks ranked by composite impact score,
// highlighting bottleneck nodes that sit on many dependency paths.
type ImpactReportStrategy struct{}

// Render produces an impact-ranked report with bottleneck highlights.
func (s ImpactReportStrategy) Render(d *DAG, _ []Track) string {
	ids := d.Nodes()
	if len(ids) == 0 {
		return "No tasks in graph."
	}

	// Sort by impact descending, alphabetical tiebreaker.
	sort.Slice(ids, func(i, j int) bool {
		si := d.Node(ids[i]).Impact
		sj := d.Node(ids[j]).Impact
		if si != sj {
			return si > sj
		}
		return ids[i] < ids[j]
	})

	// Determine a bottleneck threshold: top 20% of unique scores,
	// but at least the single highest-impact node.
	threshold := bottleneckThreshold(d, ids)

	var b strings.Builder
	b.WriteString("# Impact Report\n\n")
	for rank, id := range ids {
		node := d.Node(id)
		marker := ""
		if node.Impact >= threshold && node.Impact > 0 {
			marker = " ⚠ BOTTLENECK"
		}
		fmt.Fprintf(&b, "%d. %s  impact=%.4f%s\n", rank+1, id, node.Impact, marker)
	}
	return b.String()
}

// TrackAssignmentStrategy renders parallel tracks with their member
// tasks and aggregate statistics. Each track represents an independent
// subset of the DAG that can be assigned to a separate agent.
type TrackAssignmentStrategy struct{}

// Render produces a track-by-track assignment report.
func (s TrackAssignmentStrategy) Render(d *DAG, tracks []Track) string {
	if len(tracks) == 0 {
		return "No tracks computed. Call Analyze() first."
	}

	var b strings.Builder
	b.WriteString("# Track Assignments\n\n")
	fmt.Fprintf(&b, "Total tracks: %d\n\n", len(tracks))
	for _, tr := range tracks {
		fmt.Fprintf(&b, "## Track %d (%d tasks, impact=%.4f)\n",
			tr.ID, len(tr.NodeIDs), tr.AggregateImpact)
		for _, id := range tr.NodeIDs {
			node := d.Node(id)
			if node != nil {
				fmt.Fprintf(&b, "  - %s (priority: %d, impact: %.4f)\n",
					id, node.Priority, node.Impact)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// CriticalPathStrategy renders the longest path through the DAG,
// identifying the sequence of tasks that determines minimum total
// execution time. Includes timing estimates based on node count.
type CriticalPathStrategy struct{}

// Render produces a critical path report.
func (s CriticalPathStrategy) Render(d *DAG, _ []Track) string {
	order, err := d.TopologicalSort()
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	if len(order) == 0 {
		return "No tasks in graph."
	}

	path := computeCriticalPath(d, order)
	total := d.Len()

	var b strings.Builder
	b.WriteString("# Critical Path\n\n")
	fmt.Fprintf(&b, "Length: %d of %d total tasks (%.0f%%)\n\n",
		len(path), total, 100*float64(len(path))/float64(total))

	for step, id := range path {
		node := d.Node(id)
		arrow := ""
		if step < len(path)-1 {
			arrow = " →"
		}
		fmt.Fprintf(&b, "%d. %s (priority: %d, impact: %.4f)%s\n",
			step+1, id, node.Priority, node.Impact, arrow)
	}

	b.WriteString("\nBottleneck tasks on this path constrain total execution time.\n")
	b.WriteString("Consider parallelizing or simplifying these tasks to reduce latency.\n")
	return b.String()
}

// --- helpers ---

// sortedDeps returns the dependency IDs of a node, sorted alphabetically.
func sortedDeps(d *DAG, id string) []string {
	var deps []string
	for dep := range d.adjacency[id] {
		deps = append(deps, dep)
	}
	sort.Strings(deps)
	return deps
}

// bottleneckThreshold returns the impact score at or above which a
// node is considered a bottleneck. This is the top 20th percentile
// of impact scores, ensuring at least the highest-scored node qualifies.
func bottleneckThreshold(d *DAG, sortedIDs []string) float64 {
	if len(sortedIDs) == 0 {
		return 0
	}
	// sortedIDs is already sorted by impact descending.
	cutoff := len(sortedIDs) / 5 // top 20%
	if cutoff == 0 {
		cutoff = 1
	}
	return d.Node(sortedIDs[cutoff-1]).Impact
}

// computeCriticalPath finds the longest path through the DAG using
// dynamic programming on a topological ordering.
func computeCriticalPath(d *DAG, order []string) []string {
	dist := make(map[string]int, len(order))
	prev := make(map[string]string, len(order))
	for _, id := range order {
		dist[id] = 1
	}

	for _, v := range order {
		for dep := range d.reverse[v] {
			candidate := dist[v] + 1
			if candidate > dist[dep] {
				dist[dep] = candidate
				prev[dep] = v
			}
		}
	}

	maxDist := 0
	endNode := ""
	for _, id := range order {
		if dist[id] > maxDist {
			maxDist = dist[id]
			endNode = id
		}
	}

	path := make([]string, 0, maxDist)
	for cur := endNode; cur != ""; cur = prev[cur] {
		path = append(path, cur)
	}

	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	return path
}
