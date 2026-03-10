package tui

import (
	"strings"
	"testing"
	"time"
)

func TestBuildTreeEmpty(t *testing.T) {
	t.Parallel()
	root := BuildTree("test-nebula", nil, nil, 0)
	if root == nil {
		t.Fatal("expected non-nil root")
	}
	if root.Kind != TreeNodeNebula {
		t.Errorf("root kind = %d, want TreeNodeNebula", root.Kind)
	}
	if len(root.Children) != 0 {
		t.Errorf("expected 0 children, got %d", len(root.Children))
	}
}

func TestBuildTreeWithPhases(t *testing.T) {
	t.Parallel()
	phases := []PhaseEntry{
		{ID: "phase-1", Title: "Auth", Status: PhaseDone, CostUSD: 0.82, Cycles: 2},
		{ID: "phase-2", Title: "Database", Status: PhaseWorking, CostUSD: 0.45, Cycles: 1, MaxCycles: 5},
		{ID: "phase-3", Title: "API", Status: PhaseWaiting},
	}
	root := BuildTree("my-nebula", phases, nil, 1.27)
	if len(root.Children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(root.Children))
	}

	// Verify root metrics.
	if !strings.Contains(root.Metrics, "1/3") {
		t.Errorf("root metrics = %q, want to contain '1/3'", root.Metrics)
	}

	// Done phase should not be auto-expanded.
	if root.Children[0].Expanded {
		t.Error("done phase should not be auto-expanded")
	}

	// Working phase should be auto-expanded.
	if !root.Children[1].Expanded {
		t.Error("working phase should be auto-expanded")
	}

	// Waiting phase should not be auto-expanded.
	if root.Children[2].Expanded {
		t.Error("waiting phase should not be auto-expanded")
	}
}

func TestBuildTreeWithCyclesAndAgents(t *testing.T) {
	t.Parallel()
	phases := []PhaseEntry{
		{ID: "phase-1", Title: "Auth", Status: PhaseWorking, CostUSD: 0.82, Cycles: 2, MaxCycles: 5},
	}
	loopView := &LoopView{
		Cycles: []CycleEntry{
			{
				Number: 1,
				Agents: []AgentEntry{
					{Role: "coder", Done: true, CostUSD: 0.42, DurationMs: 18200},
					{Role: "reviewer", Done: true, CostUSD: 0.23, DurationMs: 12100, IssueCount: 1},
				},
			},
			{
				Number: 2,
				Agents: []AgentEntry{
					{Role: "coder", Done: false, StartedAt: time.Now()},
				},
			},
		},
	}
	phaseLoops := map[string]*LoopView{"phase-1": loopView}

	root := BuildTree("test", phases, phaseLoops, 0.82)
	phaseNode := root.Children[0]

	if len(phaseNode.Children) != 2 {
		t.Fatalf("expected 2 cycle children, got %d", len(phaseNode.Children))
	}

	// First cycle should have 2 agents.
	cycle1 := phaseNode.Children[0]
	if len(cycle1.Children) != 2 {
		t.Errorf("cycle 1 should have 2 agents, got %d", len(cycle1.Children))
	}
	if cycle1.Kind != TreeNodeCycle {
		t.Errorf("cycle1 kind = %d, want TreeNodeCycle", cycle1.Kind)
	}

	// Second cycle is active, should be auto-expanded.
	cycle2 := phaseNode.Children[1]
	if !cycle2.Expanded {
		t.Error("active cycle should be auto-expanded")
	}
	if !strings.Contains(cycle2.Label, "active") {
		t.Errorf("active cycle label = %q, want 'active' in label", cycle2.Label)
	}

	// Agent nodes.
	agent := cycle1.Children[0]
	if agent.Kind != TreeNodeAgent {
		t.Errorf("agent kind = %d, want TreeNodeAgent", agent.Kind)
	}
	if agent.Label != "coder" {
		t.Errorf("agent label = %q, want 'coder'", agent.Label)
	}
}

func TestFlattenVisible(t *testing.T) {
	t.Parallel()

	root := &TreeNode{
		Kind:     TreeNodeNebula,
		ID:       "root",
		Expanded: true,
		Children: []*TreeNode{
			{
				Kind:     TreeNodePhase,
				ID:       "a",
				Expanded: false, // collapsed
				Children: []*TreeNode{
					{Kind: TreeNodeCycle, ID: "a-c1"},
				},
			},
			{
				Kind:     TreeNodePhase,
				ID:       "b",
				Expanded: true, // expanded
				Children: []*TreeNode{
					{Kind: TreeNodeCycle, ID: "b-c1"},
					{Kind: TreeNodeCycle, ID: "b-c2"},
				},
			},
		},
	}

	flat := FlattenVisible(root)
	// root + a (collapsed, no children) + b + b-c1 + b-c2
	if len(flat) != 5 {
		t.Errorf("expected 5 visible nodes, got %d", len(flat))
		for i, n := range flat {
			t.Logf("  [%d] kind=%d id=%q", i, n.Kind, n.ID)
		}
	}

	// Verify order.
	wantIDs := []string{"root", "a", "b", "b-c1", "b-c2"}
	for i, wantID := range wantIDs {
		if i >= len(flat) {
			break
		}
		if flat[i].ID != wantID {
			t.Errorf("flat[%d].ID = %q, want %q", i, flat[i].ID, wantID)
		}
	}
}

func TestFlattenVisibleNil(t *testing.T) {
	t.Parallel()
	flat := FlattenVisible(nil)
	if flat != nil {
		t.Errorf("expected nil, got %v", flat)
	}
}

func TestTreeViewToggleExpand(t *testing.T) {
	t.Parallel()

	root := &TreeNode{
		Kind:     TreeNodeNebula,
		ID:       "root",
		Expanded: true,
		Children: []*TreeNode{
			{
				Kind:     TreeNodePhase,
				ID:       "a",
				Expanded: false,
				Depth:    1,
				Children: []*TreeNode{
					{Kind: TreeNodeCycle, ID: "a-c1", Depth: 2},
				},
			},
		},
	}

	tv := TreeView{Root: root}
	tv.FlatNodes = FlattenVisible(root)
	tv.Cursor = 1 // select phase "a"

	// Expand.
	tv.ToggleExpand()
	if !root.Children[0].Expanded {
		t.Error("phase 'a' should be expanded after toggle")
	}
	if len(tv.FlatNodes) != 3 {
		t.Errorf("expected 3 visible nodes after expand, got %d", len(tv.FlatNodes))
	}

	// Collapse.
	tv.ToggleExpand()
	if root.Children[0].Expanded {
		t.Error("phase 'a' should be collapsed after second toggle")
	}
	if len(tv.FlatNodes) != 2 {
		t.Errorf("expected 2 visible nodes after collapse, got %d", len(tv.FlatNodes))
	}
}

func TestTreeViewToggleExpandLeaf(t *testing.T) {
	t.Parallel()
	root := &TreeNode{
		Kind:     TreeNodeNebula,
		ID:       "root",
		Expanded: true,
		Children: []*TreeNode{
			{Kind: TreeNodeAgent, ID: "agent-1", Depth: 1},
		},
	}
	tv := TreeView{Root: root}
	tv.FlatNodes = FlattenVisible(root)
	tv.Cursor = 1 // select leaf agent

	// Toggle on leaf should be no-op.
	tv.ToggleExpand()
	if len(tv.FlatNodes) != 2 {
		t.Errorf("toggle on leaf should not change visible count, got %d", len(tv.FlatNodes))
	}
}

func TestTreeViewCollapseChildren(t *testing.T) {
	t.Parallel()
	root := &TreeNode{
		Kind:     TreeNodeNebula,
		ID:       "root",
		Expanded: true,
		Children: []*TreeNode{
			{Kind: TreeNodePhase, ID: "a", Expanded: true, Depth: 1,
				Children: []*TreeNode{
					{Kind: TreeNodeCycle, ID: "a-c1", Expanded: true, Depth: 2,
						Children: []*TreeNode{
							{Kind: TreeNodeAgent, ID: "coder", Depth: 3},
						},
					},
				},
			},
		},
	}

	tv := TreeView{Root: root}
	tv.FlatNodes = FlattenVisible(root)
	tv.Cursor = 1 // select phase "a"

	tv.CollapseChildren()

	// Cycle "a-c1" should now be collapsed.
	if root.Children[0].Children[0].Expanded {
		t.Error("cycle should be collapsed after CollapseChildren")
	}
}

func TestTreeViewZoom(t *testing.T) {
	t.Parallel()
	root := &TreeNode{
		Kind:     TreeNodeNebula,
		ID:       "root",
		Expanded: true,
		Children: []*TreeNode{
			{Kind: TreeNodePhase, ID: "a", Expanded: true, Depth: 1,
				Children: []*TreeNode{{Kind: TreeNodeCycle, ID: "a-c1", Depth: 2}}},
			{Kind: TreeNodePhase, ID: "b", Expanded: true, Depth: 1,
				Children: []*TreeNode{{Kind: TreeNodeCycle, ID: "b-c1", Depth: 2}}},
			{Kind: TreeNodePhase, ID: "c", Expanded: true, Depth: 1,
				Children: []*TreeNode{{Kind: TreeNodeCycle, ID: "c-c1", Depth: 2}}},
		},
	}

	tv := TreeView{Root: root}
	tv.FlatNodes = FlattenVisible(root)
	// Select phase "b".
	tv.Cursor = 3 // root, a, a-c1, b

	tv.Zoom()

	// "b" should be expanded, "a" and "c" collapsed.
	if root.Children[0].Expanded {
		t.Error("phase 'a' should be collapsed after zoom on 'b'")
	}
	if !root.Children[1].Expanded {
		t.Error("phase 'b' should be expanded after zoom")
	}
	if root.Children[2].Expanded {
		t.Error("phase 'c' should be collapsed after zoom on 'b'")
	}
}

func TestTreeViewSortPhasesByCost(t *testing.T) {
	t.Parallel()
	phases := []PhaseEntry{
		{ID: "cheap", CostUSD: 0.10},
		{ID: "expensive", CostUSD: 5.00},
		{ID: "medium", CostUSD: 1.00},
	}
	root := BuildTree("test", phases, nil, 6.10)

	tv := TreeView{Root: root}
	tv.FlatNodes = FlattenVisible(root)

	tv.SortPhases(TreeSortCost)

	if tv.Root.Children[0].ID != "expensive" {
		t.Errorf("first child after cost sort = %q, want 'expensive'", tv.Root.Children[0].ID)
	}
	if tv.Root.Children[1].ID != "medium" {
		t.Errorf("second child after cost sort = %q, want 'medium'", tv.Root.Children[1].ID)
	}
	if tv.Root.Children[2].ID != "cheap" {
		t.Errorf("third child after cost sort = %q, want 'cheap'", tv.Root.Children[2].ID)
	}
}

func TestTreeViewSortPhasesByDuration(t *testing.T) {
	t.Parallel()
	now := time.Now()
	phases := []PhaseEntry{
		{ID: "short", StartedAt: now.Add(-10 * time.Second), CompletedAt: now},
		{ID: "long", StartedAt: now.Add(-60 * time.Second), CompletedAt: now},
		{ID: "not-started"},
	}
	root := BuildTree("test", phases, nil, 0)

	tv := TreeView{Root: root}
	tv.FlatNodes = FlattenVisible(root)

	tv.SortPhases(TreeSortDuration)

	if tv.Root.Children[0].ID != "long" {
		t.Errorf("first child after duration sort = %q, want 'long'", tv.Root.Children[0].ID)
	}
	if tv.Root.Children[1].ID != "short" {
		t.Errorf("second child after duration sort = %q, want 'short'", tv.Root.Children[1].ID)
	}
}

func TestTreeViewMoveUpDown(t *testing.T) {
	t.Parallel()
	root := &TreeNode{
		Kind:     TreeNodeNebula,
		ID:       "root",
		Expanded: true,
		Children: []*TreeNode{
			{Kind: TreeNodePhase, ID: "a", Depth: 1},
			{Kind: TreeNodePhase, ID: "b", Depth: 1},
			{Kind: TreeNodePhase, ID: "c", Depth: 1},
		},
	}

	tv := TreeView{Root: root, Height: 10}
	tv.FlatNodes = FlattenVisible(root)

	tv.MoveDown()
	if tv.Cursor != 1 {
		t.Errorf("cursor = %d, want 1", tv.Cursor)
	}

	tv.MoveDown()
	tv.MoveDown()
	tv.MoveDown() // clamp at end
	if tv.Cursor != 3 {
		t.Errorf("cursor = %d, want 3", tv.Cursor)
	}

	tv.MoveUp()
	if tv.Cursor != 2 {
		t.Errorf("cursor = %d, want 2", tv.Cursor)
	}

	tv.MoveUp()
	tv.MoveUp()
	tv.MoveUp() // clamp at 0
	if tv.Cursor != 0 {
		t.Errorf("cursor = %d, want 0", tv.Cursor)
	}
}

func TestTreeViewSelectedNode(t *testing.T) {
	t.Parallel()
	root := &TreeNode{
		Kind:     TreeNodeNebula,
		ID:       "root",
		Expanded: true,
		Children: []*TreeNode{
			{Kind: TreeNodePhase, ID: "phase-1", Depth: 1},
		},
	}

	tv := TreeView{Root: root}
	tv.FlatNodes = FlattenVisible(root)

	// Select root.
	tv.Cursor = 0
	if tv.SelectedNode().ID != "root" {
		t.Errorf("selected = %q, want 'root'", tv.SelectedNode().ID)
	}

	// Select phase.
	tv.Cursor = 1
	if tv.SelectedNode().ID != "phase-1" {
		t.Errorf("selected = %q, want 'phase-1'", tv.SelectedNode().ID)
	}

	// Out of bounds.
	tv.Cursor = 99
	if tv.SelectedNode() != nil {
		t.Error("expected nil for out-of-bounds cursor")
	}
}

func TestTreeViewSelectedPhaseID(t *testing.T) {
	t.Parallel()
	root := &TreeNode{
		Kind:     TreeNodeNebula,
		ID:       "root",
		Expanded: true,
		Children: []*TreeNode{
			{Kind: TreeNodePhase, ID: "phase-1", Expanded: true, Depth: 1,
				Children: []*TreeNode{
					{Kind: TreeNodeCycle, ID: "cycle-1", Expanded: true, Depth: 2,
						Children: []*TreeNode{
							{Kind: TreeNodeAgent, ID: "coder", Depth: 3},
						},
					},
				},
			},
		},
	}

	tv := TreeView{Root: root}
	tv.FlatNodes = FlattenVisible(root)

	// Nebula root has no phase ID.
	tv.Cursor = 0
	if id := tv.SelectedPhaseID(); id != "" {
		t.Errorf("nebula root phase ID = %q, want empty", id)
	}

	// Phase node returns its own ID.
	tv.Cursor = 1
	if id := tv.SelectedPhaseID(); id != "phase-1" {
		t.Errorf("phase node phase ID = %q, want 'phase-1'", id)
	}

	// Cycle node returns parent phase ID.
	tv.Cursor = 2
	if id := tv.SelectedPhaseID(); id != "phase-1" {
		t.Errorf("cycle node phase ID = %q, want 'phase-1'", id)
	}

	// Agent node returns ancestor phase ID.
	tv.Cursor = 3
	if id := tv.SelectedPhaseID(); id != "phase-1" {
		t.Errorf("agent node phase ID = %q, want 'phase-1'", id)
	}
}

func TestTreeViewViewEmpty(t *testing.T) {
	t.Parallel()
	tv := TreeView{Width: 30, Height: 10}
	view := tv.View()
	if !strings.Contains(view, "PHASES") {
		t.Error("tree view should contain title 'PHASES'")
	}
	if !strings.Contains(view, "no phases") {
		t.Error("empty tree view should show 'no phases'")
	}
}

func TestTreeViewViewTooSmall(t *testing.T) {
	t.Parallel()
	tv := TreeView{Width: 3, Height: 1}
	if tv.View() != "" {
		t.Error("expected empty string for too-small tree view")
	}
}

func TestTreeViewViewWithPhases(t *testing.T) {
	t.Parallel()
	phases := []PhaseEntry{
		{ID: "auth", Title: "Authentication", Status: PhaseDone, CostUSD: 0.82, Cycles: 2},
		{ID: "db", Title: "Database", Status: PhaseWorking, CostUSD: 0.45, Cycles: 1, MaxCycles: 5},
	}
	root := BuildTree("test-nebula", phases, nil, 1.27)

	tv := TreeView{Root: root, Width: 50, Height: 20}
	tv.FlatNodes = FlattenVisible(root)

	view := tv.View()
	if !strings.Contains(view, "PHASES") {
		t.Error("should contain title")
	}
	if !strings.Contains(view, iconDone) {
		t.Error("should contain done icon")
	}
	if !strings.Contains(view, iconWorking) {
		t.Error("should contain working icon for in-progress phase")
	}
}

func TestTreeViewScrolling(t *testing.T) {
	t.Parallel()
	root := &TreeNode{
		Kind:     TreeNodeNebula,
		ID:       "root",
		Expanded: true,
	}
	for i := 0; i < 20; i++ {
		root.Children = append(root.Children, &TreeNode{
			Kind:  TreeNodePhase,
			ID:    "ph",
			Label: "Phase",
			Depth: 1,
		})
	}

	tv := TreeView{Root: root, Width: 30, Height: 8}
	tv.FlatNodes = FlattenVisible(root)

	// Move cursor past viewport.
	for i := 0; i < 10; i++ {
		tv.MoveDown()
	}

	if tv.Cursor < tv.Offset || tv.Cursor >= tv.Offset+tv.Height {
		t.Errorf("cursor %d not visible in viewport [%d, %d)", tv.Cursor, tv.Offset, tv.Offset+tv.Height)
	}

	view := tv.View()
	if !strings.Contains(view, "more") {
		t.Error("should show scroll indicator")
	}
}

func TestPhaseMetrics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		phase   PhaseEntry
		wantSub string
	}{
		{"done", PhaseEntry{Status: PhaseDone, CostUSD: 1.50, Cycles: 3}, "$1.50"},
		{"working with max", PhaseEntry{Status: PhaseWorking, CostUSD: 0.50, Cycles: 2, MaxCycles: 5}, "cycle 2/5"},
		{"working no max", PhaseEntry{Status: PhaseWorking, CostUSD: 0.50, Cycles: 2}, "cycle 2"},
		{"waiting queued", PhaseEntry{Status: PhaseWaiting}, "queued"},
		{"waiting blocked", PhaseEntry{Status: PhaseWaiting, BlockedBy: "other-phase"}, "blocked by: other-phase"},
		{"gate", PhaseEntry{Status: PhaseGate}, "awaiting approval"},
		{"skipped", PhaseEntry{Status: PhaseSkipped}, "skipped"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := phaseMetrics(&tt.phase)
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("phaseMetrics() = %q, want to contain %q", got, tt.wantSub)
			}
		})
	}
}

func TestSyncTreePreservesExpandState(t *testing.T) {
	t.Parallel()
	phases := []PhaseEntry{
		{ID: "a", Title: "Phase A", Status: PhaseDone},
		{ID: "b", Title: "Phase B", Status: PhaseDone},
	}
	tv := NewTreeView()
	tv.SyncTree("test", phases, nil, 0)

	// Manually expand phase "a".
	for _, n := range tv.FlatNodes {
		if n.ID == "a" {
			n.Expanded = true
		}
	}

	// Sync again — expand state for "a" should be preserved.
	tv.SyncTree("test", phases, nil, 0)

	var aExpanded bool
	for _, n := range tv.FlatNodes {
		if n.ID == "a" {
			aExpanded = n.Expanded
		}
	}
	if !aExpanded {
		t.Error("expand state for phase 'a' was not preserved across SyncTree")
	}
}

func TestNodeKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		node *TreeNode
		want string
	}{
		{&TreeNode{Kind: TreeNodeNebula, ID: "n"}, "nebula:n"},
		{&TreeNode{Kind: TreeNodePhase, ID: "p"}, "phase:p"},
		{&TreeNode{Kind: TreeNodeCycle, ID: "c"}, "cycle:c"},
		{&TreeNode{Kind: TreeNodeAgent, ID: "a"}, "agent:a"},
	}
	for _, tt := range tests {
		got := nodeKey(tt.node)
		if got != tt.want {
			t.Errorf("nodeKey(%v) = %q, want %q", tt.node.Kind, got, tt.want)
		}
	}
}
