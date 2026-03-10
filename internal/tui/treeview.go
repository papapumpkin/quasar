package tui

import (
	"sort"
	"time"
)

// TreeNodeKind identifies the level of a node within the hierarchical tree view.
type TreeNodeKind int

const (
	// TreeNodeNebula is the root node representing the entire nebula.
	TreeNodeNebula TreeNodeKind = iota
	// TreeNodePhase represents a single phase (expandable to show cycles).
	TreeNodePhase
	// TreeNodeCycle represents a coder-reviewer cycle within a phase.
	TreeNodeCycle
	// TreeNodeAgent represents an agent invocation within a cycle.
	TreeNodeAgent
)

// TreeMetricMode controls which set of right-aligned metrics are displayed.
type TreeMetricMode int

const (
	// TreeMetricDefault shows status, cost, and cycle count.
	TreeMetricDefault TreeMetricMode = iota
	// TreeMetricPerformance shows duration and token usage.
	TreeMetricPerformance
	// TreeMetricIssues shows issue counts and satisfaction.
	TreeMetricIssues
)

// TreeSortMode controls how phases are sorted in the tree.
type TreeSortMode int

const (
	// TreeSortNatural preserves the original phase order.
	TreeSortNatural TreeSortMode = iota
	// TreeSortCost sorts phases by descending cost.
	TreeSortCost
	// TreeSortDuration sorts phases by descending elapsed duration.
	TreeSortDuration
)

// TreeNode represents one row in the expandable tree view.
type TreeNode struct {
	Kind     TreeNodeKind
	ID       string // phase ID, cycle number, or agent role
	Label    string // display text for the node
	Status   string // icon + status text
	Metrics  string // right-aligned stats (cost, duration, issues)
	Children []*TreeNode
	Expanded bool
	Depth    int // indentation level (0 = root)

	// Source data for detail-panel sync and sorting.
	PhaseEntry *PhaseEntry // non-nil for phase nodes
	CycleEntry *CycleEntry // non-nil for cycle nodes
	AgentEntry *AgentEntry // non-nil for agent nodes
}

// TreeView renders an expandable hierarchical tree of phases, cycles, and agents
// in the sidebar, inspired by Below's cgroup view.
type TreeView struct {
	Root       *TreeNode   // nebula root node
	FlatNodes  []*TreeNode // flattened visible nodes for cursor navigation
	Cursor     int         // index into FlatNodes
	Offset     int         // scroll offset
	Width      int         // available rendering width
	Height     int         // available rendering height
	Focus      bool        // whether this view has keyboard focus
	SortMode   TreeSortMode
	MetricMode TreeMetricMode
}

// NewTreeView creates an empty tree view.
func NewTreeView() TreeView {
	return TreeView{}
}

// ToggleExpand toggles the expand/collapse state of the node at the cursor.
func (tv *TreeView) ToggleExpand() {
	if tv.Cursor < 0 || tv.Cursor >= len(tv.FlatNodes) {
		return
	}
	node := tv.FlatNodes[tv.Cursor]
	if len(node.Children) == 0 {
		return // leaf nodes don't expand
	}
	node.Expanded = !node.Expanded
	tv.rebuildFlat()
}

// CollapseChildren collapses children of the selected node (Below's `=` key).
func (tv *TreeView) CollapseChildren() {
	if tv.Cursor < 0 || tv.Cursor >= len(tv.FlatNodes) {
		return
	}
	node := tv.FlatNodes[tv.Cursor]
	for _, child := range node.Children {
		child.Expanded = false
	}
	tv.rebuildFlat()
}

// Zoom expands the selected node and collapses all siblings (Below's `z` key).
func (tv *TreeView) Zoom() {
	if tv.Cursor < 0 || tv.Cursor >= len(tv.FlatNodes) {
		return
	}
	selected := tv.FlatNodes[tv.Cursor]

	// Find siblings by walking the parent's children.
	parent := tv.findParent(selected)
	if parent == nil {
		return
	}
	for _, child := range parent.Children {
		child.Expanded = child == selected
	}
	selected.Expanded = true
	tv.rebuildFlat()
}

// findParent walks the tree to find the parent of the given node.
func (tv *TreeView) findParent(target *TreeNode) *TreeNode {
	if tv.Root == nil {
		return nil
	}
	return findParentRecursive(tv.Root, target)
}

// findParentRecursive searches for the parent of target in the subtree rooted at node.
func findParentRecursive(node, target *TreeNode) *TreeNode {
	for _, child := range node.Children {
		if child == target {
			return node
		}
		if found := findParentRecursive(child, target); found != nil {
			return found
		}
	}
	return nil
}

// SortPhases sorts the phase children of the root node by the given mode.
func (tv *TreeView) SortPhases(mode TreeSortMode) {
	tv.SortMode = mode
	if tv.Root == nil {
		return
	}
	switch mode {
	case TreeSortCost:
		sort.SliceStable(tv.Root.Children, func(i, j int) bool {
			ci := phaseNodeCost(tv.Root.Children[i])
			cj := phaseNodeCost(tv.Root.Children[j])
			return ci > cj // descending
		})
	case TreeSortDuration:
		sort.SliceStable(tv.Root.Children, func(i, j int) bool {
			di := phaseNodeDuration(tv.Root.Children[i])
			dj := phaseNodeDuration(tv.Root.Children[j])
			return di > dj // descending
		})
	}
	tv.rebuildFlat()
}

// phaseNodeCost returns the cost for a phase node, or 0 if not a phase.
func phaseNodeCost(n *TreeNode) float64 {
	if n.PhaseEntry != nil {
		return n.PhaseEntry.CostUSD
	}
	return 0
}

// phaseNodeDuration returns the elapsed duration for a phase node in milliseconds.
func phaseNodeDuration(n *TreeNode) int64 {
	if n.PhaseEntry == nil {
		return 0
	}
	p := n.PhaseEntry
	if p.StartedAt.IsZero() {
		return 0
	}
	if !p.CompletedAt.IsZero() {
		return p.CompletedAt.Sub(p.StartedAt).Milliseconds()
	}
	// In-progress: use elapsed time so sorting by duration ranks active phases correctly.
	return time.Since(p.StartedAt).Milliseconds()
}

// rebuildFlat reconstructs FlatNodes from the tree and clamps the cursor.
func (tv *TreeView) rebuildFlat() {
	tv.FlatNodes = FlattenVisible(tv.Root)
	if max := len(tv.FlatNodes) - 1; max >= 0 {
		if tv.Cursor > max {
			tv.Cursor = max
		}
	} else {
		tv.Cursor = 0
	}
	tv.ensureVisible()
}

// MoveUp moves the cursor up by one, clamping at 0.
func (tv *TreeView) MoveUp() {
	if tv.Cursor > 0 {
		tv.Cursor--
	}
	tv.ensureVisible()
}

// MoveDown moves the cursor down by one, clamping at the last visible node.
func (tv *TreeView) MoveDown() {
	max := len(tv.FlatNodes) - 1
	if max < 0 {
		max = 0
	}
	if tv.Cursor < max {
		tv.Cursor++
	}
	tv.ensureVisible()
}

// ensureVisible adjusts the scroll offset so the cursor is within the viewport.
func (tv *TreeView) ensureVisible() {
	if tv.Height <= 0 {
		return
	}
	if tv.Cursor < tv.Offset {
		tv.Offset = tv.Cursor
	}
	if tv.Cursor >= tv.Offset+tv.Height {
		tv.Offset = tv.Cursor - tv.Height + 1
	}
}

// SelectedNode returns the currently selected tree node, or nil.
func (tv *TreeView) SelectedNode() *TreeNode {
	if tv.Cursor < 0 || tv.Cursor >= len(tv.FlatNodes) {
		return nil
	}
	return tv.FlatNodes[tv.Cursor]
}

// SelectedPhaseID returns the phase ID of the selected node.
// For cycle and agent nodes, this walks up to find the parent phase.
func (tv *TreeView) SelectedPhaseID() string {
	node := tv.SelectedNode()
	if node == nil {
		return ""
	}
	switch node.Kind {
	case TreeNodePhase:
		return node.ID
	case TreeNodeCycle, TreeNodeAgent:
		// Walk up to find phase parent.
		return tv.findAncestorPhaseID(node)
	default:
		return ""
	}
}

// findAncestorPhaseID walks the tree to find the phase ancestor of the given node.
func (tv *TreeView) findAncestorPhaseID(target *TreeNode) string {
	if tv.Root == nil {
		return ""
	}
	_, id := findAncestorPhase(tv.Root, target)
	return id
}

// findAncestorPhase returns the phase ID of the nearest phase ancestor.
func findAncestorPhase(node, target *TreeNode) (found bool, phaseID string) {
	for _, child := range node.Children {
		if child == target {
			if node.Kind == TreeNodePhase {
				return true, node.ID
			}
			return true, ""
		}
		if f, id := findAncestorPhase(child, target); f {
			if id != "" {
				return true, id
			}
			if node.Kind == TreeNodePhase {
				return true, node.ID
			}
			return true, ""
		}
	}
	return false, ""
}

// SyncTree rebuilds the tree from the current data and preserves expand state.
func (tv *TreeView) SyncTree(nebulaName string, phases []PhaseEntry, phaseLoops map[string]*LoopView, totalCost float64) {
	// Capture current expand state.
	expandState := make(map[string]bool)
	if tv.Root != nil {
		captureExpandState(tv.Root, expandState)
	}

	tv.Root = BuildTree(nebulaName, phases, phaseLoops, totalCost)

	// Restore expand state.
	restoreExpandState(tv.Root, expandState)

	// Auto-expand active phases (override saved state).
	autoExpandActive(tv.Root)

	// Apply current sort.
	if tv.SortMode != TreeSortNatural {
		tv.SortPhases(tv.SortMode)
	}

	tv.rebuildFlat()
}
