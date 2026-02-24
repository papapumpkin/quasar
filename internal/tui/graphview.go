package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/papapumpkin/quasar/internal/dag"
	"github.com/papapumpkin/quasar/internal/ui"
)

// GraphView renders the phase DAG in the cockpit using the DAGRenderer.
// It re-renders on phase status changes and supports vertical scrolling
// for large graphs. Nodes are selectable for drill-down into phase loops.
type GraphView struct {
	renderer *ui.DAGRenderer
	waves    []dag.Wave
	deps     map[string][]string // phaseID → dependency IDs
	titles   map[string]string   // phaseID → display title
	statuses map[string]PhaseStatus
	nodeIDs  []string // ordered list of all node IDs for cursor navigation
	cursor   int      // index into nodeIDs
	viewport viewport.Model
	width    int
	height   int
	ready    bool // whether the viewport has been initialized with dimensions

	// Toggle state.
	showTracks       bool
	showCriticalPath bool
}

// NewGraphView creates a GraphView from phase info.
func NewGraphView(phases []PhaseInfo, width, height int) GraphView {
	deps := make(map[string][]string, len(phases))
	titles := make(map[string]string, len(phases))
	statuses := make(map[string]PhaseStatus, len(phases))
	var nodeIDs []string

	for _, p := range phases {
		deps[p.ID] = p.DependsOn
		titles[p.ID] = p.Title
		if p.Status != 0 {
			statuses[p.ID] = p.Status
		} else {
			statuses[p.ID] = PhaseWaiting
		}
		nodeIDs = append(nodeIDs, p.ID)
	}

	// Build DAG to compute waves.
	d := dag.New()
	for _, p := range phases {
		d.AddNodeIdempotent(p.ID, 0)
	}
	for _, p := range phases {
		for _, dep := range p.DependsOn {
			if err := d.AddEdge(p.ID, dep); err != nil {
				fmt.Fprintf(os.Stderr, "graphview: AddEdge(%s, %s): %v\n", p.ID, dep, err)
			}
		}
	}
	waves, err := d.ComputeWaves()
	if err != nil {
		fmt.Fprintf(os.Stderr, "graphview: ComputeWaves: %v\n", err)
	}

	// Reorder nodeIDs to follow wave order for intuitive cursor navigation.
	ordered := make([]string, 0, len(nodeIDs))
	for _, w := range waves {
		ordered = append(ordered, w.NodeIDs...)
	}
	if len(ordered) > 0 {
		nodeIDs = ordered
	}

	gv := GraphView{
		renderer: &ui.DAGRenderer{
			Width:    width,
			UseColor: true,
		},
		waves:    waves,
		deps:     deps,
		titles:   titles,
		statuses: statuses,
		nodeIDs:  nodeIDs,
		width:    width,
		height:   height,
	}

	gv.initViewport()
	return gv
}

// initViewport sets up the viewport and renders the initial DAG content.
func (gv *GraphView) initViewport() {
	gv.viewport = viewport.New(gv.width, gv.height)
	gv.viewport.SetContent(gv.renderDAG())
	gv.ready = true
}

// SetSize updates the view dimensions and re-renders.
func (gv *GraphView) SetSize(width, height int) {
	gv.width = width
	gv.height = height
	if gv.renderer == nil {
		return
	}
	gv.renderer.Width = width
	gv.viewport.Width = width
	gv.viewport.Height = height
	gv.viewport.SetContent(gv.renderDAG())
}

// SetPhaseStatus updates the status of a phase and re-renders the DAG.
func (gv *GraphView) SetPhaseStatus(phaseID string, status PhaseStatus) {
	if gv.statuses == nil {
		return
	}
	gv.statuses[phaseID] = status
	gv.viewport.SetContent(gv.renderDAG())
}

// AppendPhase adds a hot-added phase to the graph and rebuilds the DAG layout.
func (gv *GraphView) AppendPhase(p PhaseInfo) {
	if gv.statuses == nil {
		gv.statuses = make(map[string]PhaseStatus)
		gv.deps = make(map[string][]string)
		gv.titles = make(map[string]string)
	}

	gv.deps[p.ID] = p.DependsOn
	gv.titles[p.ID] = p.Title
	gv.statuses[p.ID] = PhaseWaiting
	gv.nodeIDs = append(gv.nodeIDs, p.ID)

	// Rebuild the DAG to recompute waves with the new phase.
	d := dag.New()
	for id := range gv.statuses {
		d.AddNodeIdempotent(id, 0)
	}
	for id, depList := range gv.deps {
		for _, dep := range depList {
			if err := d.AddEdge(id, dep); err != nil {
				fmt.Fprintf(os.Stderr, "graphview: AppendPhase AddEdge(%s, %s): %v\n", id, dep, err)
			}
		}
	}
	waves, err := d.ComputeWaves()
	if err != nil {
		fmt.Fprintf(os.Stderr, "graphview: AppendPhase ComputeWaves: %v\n", err)
	}
	gv.waves = waves

	// Reorder nodeIDs to follow wave order.
	ordered := make([]string, 0, len(gv.nodeIDs))
	for _, w := range waves {
		ordered = append(ordered, w.NodeIDs...)
	}
	if len(ordered) > 0 {
		gv.nodeIDs = ordered
	}

	// Re-render the viewport.
	if gv.renderer != nil {
		gv.viewport.SetContent(gv.renderDAG())
	}
}

// SelectedPhaseID returns the phase ID at the current cursor position,
// or empty string if no phases exist.
func (gv *GraphView) SelectedPhaseID() string {
	if len(gv.nodeIDs) == 0 {
		return ""
	}
	if gv.cursor < 0 || gv.cursor >= len(gv.nodeIDs) {
		return ""
	}
	return gv.nodeIDs[gv.cursor]
}

// MoveUp moves the cursor to the previous node in wave order.
func (gv *GraphView) MoveUp() {
	if gv.cursor > 0 {
		gv.cursor--
	}
	if gv.renderer != nil {
		gv.viewport.SetContent(gv.renderDAG())
	}
}

// MoveDown moves the cursor to the next node in wave order.
func (gv *GraphView) MoveDown() {
	if gv.cursor < len(gv.nodeIDs)-1 {
		gv.cursor++
	}
	if gv.renderer != nil {
		gv.viewport.SetContent(gv.renderDAG())
	}
}

// ToggleTracks toggles track highlighting on/off.
func (gv *GraphView) ToggleTracks() {
	if gv.renderer == nil {
		return
	}
	gv.showTracks = !gv.showTracks
	if gv.showTracks {
		gv.renderer.TrackMap = gv.buildTrackMap()
	} else {
		gv.renderer.TrackMap = nil
	}
	gv.viewport.SetContent(gv.renderDAG())
}

// ToggleCriticalPath toggles critical path highlighting on/off.
func (gv *GraphView) ToggleCriticalPath() {
	if gv.renderer == nil {
		return
	}
	gv.showCriticalPath = !gv.showCriticalPath
	if gv.showCriticalPath {
		gv.renderer.CriticalPath = gv.computeCriticalPath()
	} else {
		gv.renderer.CriticalPath = nil
	}
	gv.viewport.SetContent(gv.renderDAG())
}

// Update delegates scroll keys to the viewport.
func (gv *GraphView) Update(msg tea.KeyMsg) {
	gv.viewport, _ = gv.viewport.Update(msg)
}

// View renders the graph view content.
func (gv GraphView) View() string {
	if gv.renderer == nil || len(gv.waves) == 0 {
		return emptyGraphPlaceholder(gv.width)
	}
	return gv.viewport.View()
}

// renderDAG produces the DAG string with cursor highlighting applied.
func (gv *GraphView) renderDAG() string {
	selectedID := gv.SelectedPhaseID()

	// Configure the status function to map PhaseStatus to DAGRenderer states.
	gv.renderer.StatusFunc = func(id string) ui.NodeStatus {
		status, ok := gv.statuses[id]
		if !ok {
			status = PhaseWaiting
		}
		return ui.NodeStatus{
			State: phaseStatusToDAGState(status),
		}
	}

	rendered := gv.renderer.Render(gv.waves, gv.deps, gv.titles)

	// Append a legend and cursor indicator below the graph.
	var sb strings.Builder
	sb.WriteString(rendered)
	sb.WriteByte('\n')

	// Cursor indicator showing the selected node.
	if selectedID != "" {
		title := gv.titles[selectedID]
		if title == "" {
			title = selectedID
		}
		status := gv.statuses[selectedID]
		indicator := lipgloss.NewStyle().
			Foreground(colorNebula).
			Bold(true).
			Render("▸ " + title)
		stateLabel := lipgloss.NewStyle().
			Foreground(phaseStatusColor(status)).
			Render(" [" + phaseStatusToDAGState(status) + "]")
		sb.WriteString(indicator + stateLabel)
		sb.WriteByte('\n')
	}

	// Legend line.
	sb.WriteByte('\n')
	legend := graphLegend()
	sb.WriteString(legend)

	// Toggle state indicators.
	var toggles []string
	if gv.showTracks {
		toggles = append(toggles, "tracks: on")
	}
	if gv.showCriticalPath {
		toggles = append(toggles, "critical path: on")
	}
	if len(toggles) > 0 {
		sb.WriteByte('\n')
		sb.WriteString(lipgloss.NewStyle().
			Foreground(colorMutedLight).
			Render("  " + strings.Join(toggles, "  │  ")))
	}

	return sb.String()
}

// graphLegend returns a styled legend line for the graph.
func graphLegend() string {
	items := []struct {
		label string
		color lipgloss.Color
	}{
		{"queued", colorPrimary},
		{"running", colorStarYellow},
		{"done", colorSuccess},
		{"failed", colorDanger},
	}

	var parts []string
	for _, item := range items {
		dot := lipgloss.NewStyle().Foreground(item.color).Render("●")
		label := lipgloss.NewStyle().Foreground(colorMutedLight).Render(item.label)
		parts = append(parts, dot+" "+label)
	}
	return "  " + strings.Join(parts, "  ")
}

// emptyGraphPlaceholder renders the empty state for the graph tab.
func emptyGraphPlaceholder(width int) string {
	msg := "No graph data available"
	style := lipgloss.NewStyle().
		Foreground(colorMuted).
		Width(width).
		Align(lipgloss.Center).
		PaddingTop(2)
	return style.Render(msg)
}

// phaseStatusToDAGState maps TUI PhaseStatus to DAGRenderer state strings.
func phaseStatusToDAGState(s PhaseStatus) string {
	switch s {
	case PhaseWorking:
		return "running"
	case PhaseDone:
		return "done"
	case PhaseFailed:
		return "failed"
	case PhaseGate:
		return "blocked"
	case PhaseSkipped:
		return "done"
	default:
		return "queued"
	}
}

// phaseStatusColor returns the lipgloss color for a phase status.
func phaseStatusColor(s PhaseStatus) lipgloss.Color {
	switch s {
	case PhaseWorking:
		return colorStarYellow
	case PhaseDone:
		return colorSuccess
	case PhaseFailed:
		return colorDanger
	case PhaseGate:
		return colorAccent
	default:
		return colorPrimary
	}
}

// buildTrackMap creates a simple track map grouping nodes by their wave number.
func (gv *GraphView) buildTrackMap() map[string]int {
	tm := make(map[string]int, len(gv.nodeIDs))
	for _, w := range gv.waves {
		for _, id := range w.NodeIDs {
			tm[id] = w.Number
		}
	}
	return tm
}

// computeCriticalPath finds the longest path through the DAG by counting
// the deepest dependency chain for each node. Nodes on the longest chain
// are included in the critical path set.
func (gv *GraphView) computeCriticalPath() map[string]bool {
	if len(gv.nodeIDs) == 0 {
		return nil
	}

	// Build reverse adjacency: who depends on whom.
	children := make(map[string][]string)
	for id, depList := range gv.deps {
		for _, dep := range depList {
			children[dep] = append(children[dep], id)
		}
	}

	// Compute depth for each node (longest path from a root).
	depth := make(map[string]int, len(gv.nodeIDs))
	var computeDepth func(id string) int
	computeDepth = func(id string) int {
		if d, ok := depth[id]; ok {
			return d
		}
		maxDep := 0
		for _, dep := range gv.deps[id] {
			d := computeDepth(dep) + 1
			if d > maxDep {
				maxDep = d
			}
		}
		depth[id] = maxDep
		return maxDep
	}
	for _, id := range gv.nodeIDs {
		computeDepth(id)
	}

	// Find the node with maximum depth.
	maxDepth := 0
	var deepestNode string
	for id, d := range depth {
		if d > maxDepth {
			maxDepth = d
			deepestNode = id
		}
	}

	// Trace back from the deepest node along the longest chain.
	critical := make(map[string]bool)
	node := deepestNode
	for node != "" {
		critical[node] = true
		// Find the dependency with the greatest depth.
		best := ""
		bestDepth := -1
		for _, dep := range gv.deps[node] {
			if depth[dep] > bestDepth {
				bestDepth = depth[dep]
				best = dep
			}
		}
		node = best
	}

	return critical
}
