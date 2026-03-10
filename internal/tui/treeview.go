package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
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
	PhaseEntry *PhaseEntry  // non-nil for phase nodes
	CycleEntry *CycleEntry // non-nil for cycle nodes
	AgentEntry *AgentEntry // non-nil for agent nodes
}

// TreeView renders an expandable hierarchical tree of phases, cycles, and agents
// in the sidebar, inspired by Below's cgroup view.
type TreeView struct {
	Root       *TreeNode    // nebula root node
	FlatNodes  []*TreeNode  // flattened visible nodes for cursor navigation
	Cursor     int          // index into FlatNodes
	Offset     int          // scroll offset
	Width      int          // available rendering width
	Height     int          // available rendering height
	Focus      bool         // whether this view has keyboard focus
	SortMode   TreeSortMode
	MetricMode TreeMetricMode
}

// NewTreeView creates an empty tree view.
func NewTreeView() TreeView {
	return TreeView{}
}

// BuildTree constructs a tree from the current phase entries and their loop views.
// It auto-expands phases that are actively working.
func BuildTree(nebulaName string, phases []PhaseEntry, phaseLoops map[string]*LoopView, totalCost float64) *TreeNode {
	root := &TreeNode{
		Kind:     TreeNodeNebula,
		ID:       nebulaName,
		Label:    nebulaName,
		Expanded: true,
		Depth:    0,
	}

	doneCount := 0
	for _, p := range phases {
		if p.Status == PhaseDone || p.Status == PhaseSkipped {
			doneCount++
		}
	}
	root.Metrics = fmt.Sprintf("%d/%d · $%.2f", doneCount, len(phases), totalCost)

	for i := range phases {
		p := &phases[i]
		phaseNode := buildPhaseNode(p, phaseLoops[p.ID])
		phaseNode.Depth = 1
		root.Children = append(root.Children, phaseNode)
	}

	return root
}

// buildPhaseNode constructs a tree node for a single phase, including
// cycle and agent children from the loop view if available.
func buildPhaseNode(p *PhaseEntry, lv *LoopView) *TreeNode {
	label := p.Title
	if label == "" {
		label = p.ID
	}

	node := &TreeNode{
		Kind:       TreeNodePhase,
		ID:         p.ID,
		Label:      label,
		Status:     phaseStatusIcon(p.Status),
		PhaseEntry: p,
		// Auto-expand active phases.
		Expanded: p.Status == PhaseWorking || p.Status == PhaseGate,
	}

	// Build metrics string.
	node.Metrics = phaseMetrics(p)

	if lv == nil {
		return node
	}

	// Add cycle children.
	for ci := range lv.Cycles {
		c := &lv.Cycles[ci]
		cycleNode := buildCycleNode(c, ci == len(lv.Cycles)-1 && p.Status == PhaseWorking)
		cycleNode.Depth = 2
		node.Children = append(node.Children, cycleNode)
	}

	return node
}

// buildCycleNode constructs a tree node for a single cycle.
func buildCycleNode(c *CycleEntry, isActive bool) *TreeNode {
	label := fmt.Sprintf("cycle %d", c.Number)
	if isActive {
		label += " (active)"
	}

	var costSum float64
	for _, a := range c.Agents {
		costSum += a.CostUSD
	}

	node := &TreeNode{
		Kind:       TreeNodeCycle,
		ID:         fmt.Sprintf("cycle-%d", c.Number),
		Label:      label,
		Metrics:    fmt.Sprintf("$%.2f", costSum),
		CycleEntry: c,
		Expanded:   isActive,
		Depth:      2,
	}

	for ai := range c.Agents {
		a := &c.Agents[ai]
		agentNode := buildAgentNode(a)
		agentNode.Depth = 3
		node.Children = append(node.Children, agentNode)
	}

	return node
}

// buildAgentNode constructs a tree node for an agent invocation.
func buildAgentNode(a *AgentEntry) *TreeNode {
	var status, metrics string

	if a.Done {
		status = iconDone
		secs := float64(a.DurationMs) / 1000.0
		metrics = fmt.Sprintf("$%.2f  %.1fs", a.CostUSD, secs)
		if a.Role == "reviewer" && a.IssueCount > 0 {
			metrics += fmt.Sprintf("  ⚠ %d issue(s)", a.IssueCount)
		}
		if a.Role == "reviewer" && a.Satisfaction != SatisfactionNone {
			metrics += "  " + satisfactionText(a.Satisfaction)
		}
	} else {
		status = iconWorking
		metrics = a.Role + "ing..."
	}

	return &TreeNode{
		Kind:       TreeNodeAgent,
		ID:         a.Role,
		Label:      a.Role,
		Status:     status,
		Metrics:    metrics,
		AgentEntry: a,
	}
}

// satisfactionText returns a plain-text satisfaction indicator.
func satisfactionText(s Satisfaction) string {
	switch s {
	case SatisfactionGreen:
		return "✓ approved"
	case SatisfactionYellow:
		return "⚠ issues"
	case SatisfactionRed:
		return "✗ rejected"
	default:
		return ""
	}
}

// phaseMetrics builds the right-aligned metrics string for a phase node.
func phaseMetrics(p *PhaseEntry) string {
	switch p.Status {
	case PhaseDone, PhaseFailed:
		return fmt.Sprintf("$%.2f  %d cycles", p.CostUSD, p.Cycles)
	case PhaseWorking:
		if p.MaxCycles > 0 {
			return fmt.Sprintf("$%.2f  cycle %d/%d", p.CostUSD, p.Cycles, p.MaxCycles)
		}
		return fmt.Sprintf("$%.2f  cycle %d", p.CostUSD, p.Cycles)
	case PhaseWaiting:
		if p.BlockedBy != "" {
			return "blocked by: " + p.BlockedBy
		}
		return "queued"
	case PhaseGate:
		return "awaiting approval"
	case PhaseSkipped:
		return "skipped"
	default:
		return ""
	}
}

// FlattenVisible builds a flat list of visible nodes from the tree, respecting
// expand/collapse state. This is the canonical list used for cursor navigation.
func FlattenVisible(root *TreeNode) []*TreeNode {
	if root == nil {
		return nil
	}
	var result []*TreeNode
	flattenRecursive(root, &result)
	return result
}

// flattenRecursive appends the node and its visible children to the result slice.
func flattenRecursive(node *TreeNode, result *[]*TreeNode) {
	*result = append(*result, node)
	if !node.Expanded {
		return
	}
	for _, child := range node.Children {
		flattenRecursive(child, result)
	}
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
	return 0
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

// captureExpandState records the expand state of all nodes keyed by a path-like ID.
func captureExpandState(node *TreeNode, state map[string]bool) {
	key := nodeKey(node)
	state[key] = node.Expanded
	for _, child := range node.Children {
		captureExpandState(child, state)
	}
}

// restoreExpandState applies saved expand states to the rebuilt tree.
func restoreExpandState(node *TreeNode, state map[string]bool) {
	key := nodeKey(node)
	if expanded, ok := state[key]; ok {
		node.Expanded = expanded
	}
	for _, child := range node.Children {
		restoreExpandState(child, state)
	}
}

// autoExpandActive ensures actively working phases are expanded.
func autoExpandActive(node *TreeNode) {
	if node.Kind == TreeNodePhase && node.PhaseEntry != nil {
		if node.PhaseEntry.Status == PhaseWorking || node.PhaseEntry.Status == PhaseGate {
			node.Expanded = true
			// Also expand the last cycle.
			if len(node.Children) > 0 {
				node.Children[len(node.Children)-1].Expanded = true
			}
		}
	}
	for _, child := range node.Children {
		autoExpandActive(child)
	}
}

// nodeKey returns a stable key for a tree node used for expand-state persistence.
func nodeKey(node *TreeNode) string {
	switch node.Kind {
	case TreeNodeNebula:
		return "nebula:" + node.ID
	case TreeNodePhase:
		return "phase:" + node.ID
	case TreeNodeCycle:
		return "cycle:" + node.ID
	case TreeNodeAgent:
		return "agent:" + node.ID
	default:
		return node.ID
	}
}

// Tree connector characters with trailing space for the tree view.
// treeConnectorMid and treeConnectorLast (without trailing space) are in beadview.go.
const (
	tvConnectorMid  = "├─ "
	tvConnectorLast = "└─ "
	tvConnectorPipe = "│  "
)

// View renders the tree view as a bordered panel for the sidebar.
func (tv TreeView) View() string {
	if tv.Width < 4 || tv.Height < 1 {
		return ""
	}

	// Title.
	titleStyle := lipgloss.NewStyle().
		Foreground(colorPrimary).
		Bold(true)
	title := titleStyle.Render("PHASES")

	// Inner width excludes border (2) and padding (2).
	innerWidth := tv.Width - 4
	if innerWidth < 4 {
		innerWidth = 4
	}

	var rows []string
	rows = append(rows, title)
	rows = append(rows, "") // spacing after title

	if len(tv.FlatNodes) == 0 {
		empty := lipgloss.NewStyle().Foreground(colorMuted).Render("  (no phases)")
		rows = append(rows, empty)
	} else {
		// Available lines for tree entries (height minus title and spacing).
		visibleLines := tv.Height - 2
		if visibleLines < 1 {
			visibleLines = 1
		}

		// Top scroll indicator.
		if tv.Offset > 0 {
			indicator := lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
				Render(fmt.Sprintf("  ↑ %d more", tv.Offset))
			rows = append(rows, indicator)
			visibleLines--
		}

		// Reserve a line for bottom indicator if needed.
		remaining := len(tv.FlatNodes) - tv.Offset
		hasBottomIndicator := remaining > visibleLines
		if hasBottomIndicator {
			visibleLines--
		}

		for i := tv.Offset; i < len(tv.FlatNodes) && i < tv.Offset+visibleLines; i++ {
			node := tv.FlatNodes[i]
			row := tv.renderTreeRow(node, i == tv.Cursor, innerWidth)
			rows = append(rows, row)
		}

		// Bottom scroll indicator.
		if hasBottomIndicator {
			belowCount := len(tv.FlatNodes) - (tv.Offset + visibleLines)
			indicator := lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
				Render(fmt.Sprintf("  ↓ %d more", belowCount))
			rows = append(rows, indicator)
		}
	}

	content := strings.Join(rows, "\n")

	borderStyle := panelStyle(tv.Focus).
		Width(tv.Width - 2). // subtract border width
		Height(tv.Height)

	return borderStyle.Render(content)
}

// renderTreeRow renders a single node row with tree-drawing characters.
func (tv TreeView) renderTreeRow(node *TreeNode, selected bool, maxWidth int) string {
	// Build indent prefix with tree connectors.
	indent := tv.buildIndent(node)

	// Status icon.
	iconStr := ""
	if node.Status != "" {
		iconStr = tv.styleNodeIcon(node) + " "
	}

	// Expand/collapse indicator for nodes with children.
	expandIndicator := ""
	if len(node.Children) > 0 {
		if node.Expanded {
			expandIndicator = "▾ "
		} else {
			expandIndicator = "▸ "
		}
	}

	// Label — truncate to fit.
	label := node.Label

	// Calculate space used by non-label elements.
	// indent + icon + expand + label + spacing + metrics
	indentWidth := lipgloss.Width(indent)
	iconWidth := lipgloss.Width(iconStr)
	expandWidth := lipgloss.Width(expandIndicator)

	metricsStr := ""
	metricsWidth := 0
	if node.Metrics != "" && node.Kind != TreeNodeNebula {
		metricsStr = node.Metrics
		metricsWidth = lipgloss.Width(metricsStr) + 1 // +1 for spacing
	}

	labelWidth := maxWidth - indentWidth - iconWidth - expandWidth - metricsWidth
	if labelWidth < 4 {
		labelWidth = 4
	}
	label = TruncateWithEllipsis(label, labelWidth)

	// Build the row.
	var row string
	if selected {
		indicator := styleSelectionIndicator.Render(selectionIndicator)
		// Replace first chars of indent with indicator.
		styledLabel := styleRowSelected.Render(label)
		styledExpand := lipgloss.NewStyle().Foreground(colorPrimary).Render(expandIndicator)
		row = indicator + indent[lipgloss.Width(indicator):] + iconStr + styledExpand + styledLabel
	} else {
		styledLabel := tv.styleLabelForNode(node, label)
		styledExpand := lipgloss.NewStyle().Foreground(colorMuted).Render(expandIndicator)
		row = indent + iconStr + styledExpand + styledLabel
	}

	// Append right-aligned metrics.
	if metricsStr != "" {
		rowWidth := lipgloss.Width(row)
		gap := maxWidth - rowWidth - lipgloss.Width(metricsStr)
		if gap < 1 {
			gap = 1
		}
		styledMetrics := stylePhaseDetail.Render(metricsStr)
		row += strings.Repeat(" ", gap) + styledMetrics
	}

	// Pad and highlight selected row.
	if selected {
		visible := lipgloss.Width(row)
		if visible < maxWidth {
			row += strings.Repeat(" ", maxWidth-visible)
		}
		row = lipgloss.NewStyle().Background(colorSelectionBg).Render(row)
	}

	return row
}

// buildIndent builds the tree indent prefix for a node based on its depth.
func (tv TreeView) buildIndent(node *TreeNode) string {
	if node.Depth == 0 {
		return "  " // root has standard indent
	}
	// Build depth-based indent.
	var b strings.Builder
	b.WriteString("  ") // base margin
	for i := 1; i < node.Depth; i++ {
		b.WriteString(styleTreeConnector.Render(tvConnectorPipe))
	}
	// Determine if this is the last child of its parent.
	if tv.isLastChild(node) {
		b.WriteString(styleTreeConnector.Render(tvConnectorLast))
	} else {
		b.WriteString(styleTreeConnector.Render(tvConnectorMid))
	}
	return b.String()
}

// isLastChild checks if a node is the last child of its parent.
func (tv TreeView) isLastChild(node *TreeNode) bool {
	parent := tv.findParent(node)
	if parent == nil || len(parent.Children) == 0 {
		return true
	}
	return parent.Children[len(parent.Children)-1] == node
}

// styleNodeIcon returns a styled status icon for the node.
func (tv TreeView) styleNodeIcon(node *TreeNode) string {
	switch node.Status {
	case iconDone:
		return styleRowDone.Render(node.Status)
	case iconFailed:
		return styleRowFailed.Render(node.Status)
	case iconWorking:
		return styleRowWorking.Render(node.Status)
	case iconGate:
		return styleRowGate.Render(node.Status)
	case iconSkipped:
		return styleRowWaiting.Render(node.Status)
	default:
		return styleRowWaiting.Render(node.Status)
	}
}

// styleLabelForNode returns a styled label based on node kind and status.
func (tv TreeView) styleLabelForNode(node *TreeNode, label string) string {
	switch node.Kind {
	case TreeNodeNebula:
		return lipgloss.NewStyle().Foreground(colorPrimary).Bold(true).Render(label)
	case TreeNodePhase:
		if node.PhaseEntry != nil {
			return phaseStatusStyle(node.PhaseEntry.Status).Render(label)
		}
		return styleRowNormal.Render(label)
	case TreeNodeCycle:
		return styleRowNormal.Render(label)
	case TreeNodeAgent:
		if node.AgentEntry != nil && node.AgentEntry.Done {
			return stylePhaseID.Render(label)
		}
		return styleRowWorking.Render(label)
	default:
		return styleRowNormal.Render(label)
	}
}
