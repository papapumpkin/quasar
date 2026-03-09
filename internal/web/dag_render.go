package web

import (
	"github.com/papapumpkin/quasar/internal/dag"
	"github.com/papapumpkin/quasar/internal/nebula"
)

// SVG layout constants for DAG node positioning.
const (
	dagNodeWidth  = 180
	dagNodeHeight = 50
	dagColGap     = 60
	dagRowGap     = 30
	dagPadding    = 40
)

// DAGLayout holds the computed SVG layout for a nebula dependency graph.
type DAGLayout struct {
	Nodes  []DAGNode
	Edges  []DAGEdge
	Width  int
	Height int
}

// DAGNode represents a single phase in the SVG dependency graph.
type DAGNode struct {
	ID       string
	Title    string
	Status   string
	Wave     int    // column (dependency depth)
	Row      int    // row within the wave
	X        int    // SVG x coordinate (top-left of rect)
	Y        int    // SVG y coordinate (top-left of rect)
	CSSClass string // "node--done", "node--failed", etc.
	Link     string // href to /phase/{id}
}

// DAGEdge represents a dependency arrow between two nodes.
type DAGEdge struct {
	FromX    int
	FromY    int
	ToX      int
	ToY      int
	CSSClass string // "edge--satisfied" or "edge--pending"
}

// ComputeDAGLayout assigns SVG coordinates to phases based on wave assignment.
// Phases at the same dependency depth share a column. Edges point from
// dependency to dependent (left to right in wave order).
func ComputeDAGLayout(neb *nebula.Nebula, state *nebula.State) DAGLayout {
	if neb == nil || len(neb.Phases) == 0 {
		return DAGLayout{}
	}
	if state == nil {
		state = &nebula.State{Phases: make(map[string]*nebula.PhaseState)}
	}

	waves := buildLayoutWaves(neb.Phases)
	waveForPhase := mapPhasesToWaves(waves)

	// Group phases by wave to assign row positions within each column.
	waveGroups := make(map[int][]string)
	for _, phase := range neb.Phases {
		w := waveForPhase[phase.ID]
		waveGroups[w] = append(waveGroups[w], phase.ID)
	}

	// Assign row positions and compute nodes.
	nodeByID := make(map[string]*DAGNode, len(neb.Phases))
	nodes := make([]DAGNode, 0, len(neb.Phases))

	for _, phase := range neb.Phases {
		w := waveForPhase[phase.ID]

		// Row is the index within this wave's group.
		row := 0
		for i, id := range waveGroups[w] {
			if id == phase.ID {
				row = i
				break
			}
		}

		// Wave is 1-based; subtract 1 for zero-based column index.
		col := w - 1
		if col < 0 {
			col = 0
		}

		x := dagPadding + col*(dagNodeWidth+dagColGap)
		y := dagPadding + row*(dagNodeHeight+dagRowGap)

		status := phaseStatusOrPending(state, phase.ID)
		cssClass := statusToCSSClass(status)

		node := DAGNode{
			ID:       phase.ID,
			Title:    phase.Title,
			Status:   status,
			Wave:     w,
			Row:      row,
			X:        x,
			Y:        y,
			CSSClass: cssClass,
			Link:     "/phase/" + phase.ID,
		}
		nodes = append(nodes, node)
		nodeByID[phase.ID] = &nodes[len(nodes)-1]
	}

	// Compute edges from dependency to dependent using phase specs directly.
	edges := make([]DAGEdge, 0)
	for _, phase := range neb.Phases {
		for _, dep := range phase.DependsOn {
			fromNode := nodeByID[dep]
			toNode := nodeByID[phase.ID]
			if fromNode == nil || toNode == nil {
				continue
			}

			depStatus := phaseStatusOrPending(state, dep)
			edgeClass := "edge--pending"
			if depStatus == "done" || depStatus == "skipped" {
				edgeClass = "edge--satisfied"
			}

			edges = append(edges, DAGEdge{
				FromX:    fromNode.X + dagNodeWidth,
				FromY:    fromNode.Y + dagNodeHeight/2,
				ToX:      toNode.X,
				ToY:      toNode.Y + dagNodeHeight/2,
				CSSClass: edgeClass,
			})
		}
	}

	width, height := computeViewport(nodes)

	return DAGLayout{
		Nodes:  nodes,
		Edges:  edges,
		Width:  width,
		Height: height,
	}
}

// buildLayoutWaves constructs a DAG with correct edge direction for layout
// (from dependent to dependency) and computes waves. Root nodes with no
// dependencies appear in wave 1 (leftmost column).
func buildLayoutWaves(phases []nebula.PhaseSpec) []dag.Wave {
	dg := dag.New()
	for _, p := range phases {
		dg.AddNodeIdempotent(p.ID, p.Priority)
	}
	for _, p := range phases {
		for _, dep := range p.DependsOn {
			// Edge: p.ID depends on dep (p.ID → dep in DAG semantics).
			if err := dg.AddEdge(p.ID, dep); err != nil {
				return nil
			}
		}
	}
	waves, _ := dg.ComputeWaves()
	return waves
}

// computeViewport calculates the SVG viewport size from node positions.
func computeViewport(nodes []DAGNode) (width, height int) {
	if len(nodes) == 0 {
		return 0, 0
	}
	for _, n := range nodes {
		right := n.X + dagNodeWidth + dagPadding
		bottom := n.Y + dagNodeHeight + dagPadding
		if right > width {
			width = right
		}
		if bottom > height {
			height = bottom
		}
	}
	return width, height
}

// phaseStatusOrPending returns the status string for a phase, defaulting to "pending".
func phaseStatusOrPending(state *nebula.State, phaseID string) string {
	ps := state.Phases[phaseID]
	if ps == nil {
		return "pending"
	}
	return statusString(ps.Status)
}

// statusToCSSClass maps a status string to a DAG node CSS class.
func statusToCSSClass(status string) string {
	switch status {
	case "done":
		return "node--done"
	case "in_progress", "created":
		return "node--active"
	case "failed":
		return "node--failed"
	case "skipped":
		return "node--skipped"
	case "decomposed":
		return "node--decomposed"
	default:
		return "node--pending"
	}
}
