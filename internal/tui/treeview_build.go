package tui

import "fmt"

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
