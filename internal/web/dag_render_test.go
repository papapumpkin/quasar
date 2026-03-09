package web

import (
	"testing"

	"github.com/papapumpkin/quasar/internal/nebula"
)

func TestComputeDAGLayout_LinearChain(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Phases: []nebula.PhaseSpec{
			{ID: "a", Title: "Alpha"},
			{ID: "b", Title: "Beta", DependsOn: []string{"a"}},
			{ID: "c", Title: "Gamma", DependsOn: []string{"b"}},
		},
	}
	state := &nebula.State{Phases: make(map[string]*nebula.PhaseState)}

	layout := ComputeDAGLayout(neb, state)

	if len(layout.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(layout.Nodes))
	}
	if len(layout.Edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(layout.Edges))
	}

	// In a linear chain, each node should be in a different wave (column).
	nodeByID := make(map[string]DAGNode)
	for _, n := range layout.Nodes {
		nodeByID[n.ID] = n
	}

	if nodeByID["a"].Wave == nodeByID["b"].Wave {
		t.Error("a and b should be in different waves")
	}
	if nodeByID["b"].Wave == nodeByID["c"].Wave {
		t.Error("b and c should be in different waves")
	}

	// Nodes in later waves should have larger X values.
	if nodeByID["a"].X >= nodeByID["b"].X {
		t.Errorf("a.X (%d) should be less than b.X (%d)", nodeByID["a"].X, nodeByID["b"].X)
	}
	if nodeByID["b"].X >= nodeByID["c"].X {
		t.Errorf("b.X (%d) should be less than c.X (%d)", nodeByID["b"].X, nodeByID["c"].X)
	}

	// Viewport should be large enough for all nodes.
	if layout.Width <= 0 || layout.Height <= 0 {
		t.Errorf("expected positive viewport dimensions, got %dx%d", layout.Width, layout.Height)
	}
}

func TestComputeDAGLayout_Diamond(t *testing.T) {
	t.Parallel()

	// Diamond: A -> B, A -> C, B -> D, C -> D
	neb := &nebula.Nebula{
		Phases: []nebula.PhaseSpec{
			{ID: "a", Title: "Root"},
			{ID: "b", Title: "Left", DependsOn: []string{"a"}},
			{ID: "c", Title: "Right", DependsOn: []string{"a"}},
			{ID: "d", Title: "Sink", DependsOn: []string{"b", "c"}},
		},
	}
	state := &nebula.State{Phases: make(map[string]*nebula.PhaseState)}

	layout := ComputeDAGLayout(neb, state)

	if len(layout.Nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(layout.Nodes))
	}
	if len(layout.Edges) != 4 {
		t.Fatalf("expected 4 edges, got %d", len(layout.Edges))
	}

	nodeByID := make(map[string]DAGNode)
	for _, n := range layout.Nodes {
		nodeByID[n.ID] = n
	}

	// B and C should be in the same wave (independent, both depend on A).
	if nodeByID["b"].Wave != nodeByID["c"].Wave {
		t.Errorf("b and c should share a wave, got b=%d c=%d", nodeByID["b"].Wave, nodeByID["c"].Wave)
	}

	// B and C should be in different rows within the same wave.
	if nodeByID["b"].Row == nodeByID["c"].Row {
		t.Error("b and c should have different rows within their wave")
	}

	// B and C should have different Y coordinates.
	if nodeByID["b"].Y == nodeByID["c"].Y {
		t.Error("b and c should have different Y coordinates")
	}

	// A should be in wave 1, B/C in wave 2, D in wave 3.
	if nodeByID["a"].Wave != 1 {
		t.Errorf("a should be wave 1, got %d", nodeByID["a"].Wave)
	}
	if nodeByID["d"].Wave != 3 {
		t.Errorf("d should be wave 3, got %d", nodeByID["d"].Wave)
	}
}

func TestComputeDAGLayout_DisconnectedPhases(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Phases: []nebula.PhaseSpec{
			{ID: "x", Title: "Independent X"},
			{ID: "y", Title: "Independent Y"},
			{ID: "z", Title: "Independent Z"},
		},
	}
	state := &nebula.State{Phases: make(map[string]*nebula.PhaseState)}

	layout := ComputeDAGLayout(neb, state)

	if len(layout.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(layout.Nodes))
	}

	// No dependencies means no edges.
	if len(layout.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(layout.Edges))
	}

	// All nodes should be in wave 1 (same column).
	for _, n := range layout.Nodes {
		if n.Wave != 1 {
			t.Errorf("node %s should be wave 1, got %d", n.ID, n.Wave)
		}
	}

	// All nodes should share the same X but have different Y values.
	x0 := layout.Nodes[0].X
	for _, n := range layout.Nodes[1:] {
		if n.X != x0 {
			t.Errorf("disconnected node %s should share X=%d, got %d", n.ID, x0, n.X)
		}
	}
}

func TestComputeDAGLayout_NilInputs(t *testing.T) {
	t.Parallel()

	layout := ComputeDAGLayout(nil, nil)
	if len(layout.Nodes) != 0 {
		t.Errorf("expected 0 nodes for nil nebula, got %d", len(layout.Nodes))
	}
	if len(layout.Edges) != 0 {
		t.Errorf("expected 0 edges for nil nebula, got %d", len(layout.Edges))
	}
}

func TestComputeDAGLayout_EmptyPhases(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{Phases: nil}
	layout := ComputeDAGLayout(neb, nil)
	if len(layout.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(layout.Nodes))
	}
}

func TestComputeDAGLayout_StatusColoring(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Phases: []nebula.PhaseSpec{
			{ID: "done-phase", Title: "Done"},
			{ID: "wip-phase", Title: "Working"},
			{ID: "fail-phase", Title: "Failed"},
			{ID: "skip-phase", Title: "Skipped"},
			{ID: "pending-phase", Title: "Pending"},
			{ID: "decomp-phase", Title: "Decomposed"},
		},
	}
	state := &nebula.State{
		Phases: map[string]*nebula.PhaseState{
			"done-phase":   {Status: nebula.PhaseStatusDone},
			"wip-phase":    {Status: nebula.PhaseStatusInProgress},
			"fail-phase":   {Status: nebula.PhaseStatusFailed},
			"skip-phase":   {Status: nebula.PhaseStatusSkipped},
			"decomp-phase": {Status: nebula.PhaseStatusDecomposed},
		},
	}

	layout := ComputeDAGLayout(neb, state)

	want := map[string]string{
		"done-phase":    "node--done",
		"wip-phase":     "node--active",
		"fail-phase":    "node--failed",
		"skip-phase":    "node--skipped",
		"pending-phase": "node--pending",
		"decomp-phase":  "node--decomposed",
	}

	for _, n := range layout.Nodes {
		expected, ok := want[n.ID]
		if !ok {
			continue
		}
		if n.CSSClass != expected {
			t.Errorf("node %s: expected CSS class %q, got %q", n.ID, expected, n.CSSClass)
		}
	}
}

func TestComputeDAGLayout_EdgeSatisfaction(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Phases: []nebula.PhaseSpec{
			{ID: "a", Title: "Root"},
			{ID: "b", Title: "Dep", DependsOn: []string{"a"}},
		},
	}

	t.Run("pending dependency", func(t *testing.T) {
		t.Parallel()
		state := &nebula.State{Phases: make(map[string]*nebula.PhaseState)}
		layout := ComputeDAGLayout(neb, state)
		if len(layout.Edges) != 1 {
			t.Fatalf("expected 1 edge, got %d", len(layout.Edges))
		}
		if layout.Edges[0].CSSClass != "edge--pending" {
			t.Errorf("expected edge--pending, got %q", layout.Edges[0].CSSClass)
		}
	})

	t.Run("satisfied dependency", func(t *testing.T) {
		t.Parallel()
		state := &nebula.State{
			Phases: map[string]*nebula.PhaseState{
				"a": {Status: nebula.PhaseStatusDone},
			},
		}
		layout := ComputeDAGLayout(neb, state)
		if len(layout.Edges) != 1 {
			t.Fatalf("expected 1 edge, got %d", len(layout.Edges))
		}
		if layout.Edges[0].CSSClass != "edge--satisfied" {
			t.Errorf("expected edge--satisfied, got %q", layout.Edges[0].CSSClass)
		}
	})
}

func TestComputeDAGLayout_NodeLinks(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Phases: []nebula.PhaseSpec{
			{ID: "my-phase", Title: "Test"},
		},
	}
	layout := ComputeDAGLayout(neb, nil)
	if len(layout.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(layout.Nodes))
	}
	if layout.Nodes[0].Link != "/phase/my-phase" {
		t.Errorf("expected link /phase/my-phase, got %q", layout.Nodes[0].Link)
	}
}

func TestStatusToCSSClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status string
		want   string
	}{
		{"done", "node--done"},
		{"in_progress", "node--active"},
		{"created", "node--active"},
		{"failed", "node--failed"},
		{"skipped", "node--skipped"},
		{"decomposed", "node--decomposed"},
		{"pending", "node--pending"},
		{"unknown", "node--pending"},
		{"", "node--pending"},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()
			got := statusToCSSClass(tt.status)
			if got != tt.want {
				t.Errorf("statusToCSSClass(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestComputeViewport(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		w, h := computeViewport(nil)
		if w != 0 || h != 0 {
			t.Errorf("expected 0x0, got %dx%d", w, h)
		}
	})

	t.Run("single node", func(t *testing.T) {
		t.Parallel()
		nodes := []DAGNode{{X: 40, Y: 40}}
		w, h := computeViewport(nodes)
		expectedW := 40 + dagNodeWidth + dagPadding
		expectedH := 40 + dagNodeHeight + dagPadding
		if w != expectedW {
			t.Errorf("width: expected %d, got %d", expectedW, w)
		}
		if h != expectedH {
			t.Errorf("height: expected %d, got %d", expectedH, h)
		}
	})
}
