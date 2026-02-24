package ui

import (
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/dag"
)

// helper builds waves, deps, and titles from a simple spec.
type dagSpec struct {
	id    string
	title string
	deps  []string
}

func buildTestDAG(t *testing.T, specs []dagSpec) ([]dag.Wave, map[string][]string, map[string]string) {
	t.Helper()
	d := dag.New()
	titles := make(map[string]string)
	deps := make(map[string][]string)

	for _, s := range specs {
		d.AddNodeIdempotent(s.id, 1)
		titles[s.id] = s.title
		if len(s.deps) > 0 {
			deps[s.id] = s.deps
		}
	}
	for _, s := range specs {
		for _, dep := range s.deps {
			if err := d.AddEdge(s.id, dep); err != nil {
				t.Fatalf("AddEdge(%q, %q): %v", s.id, dep, err)
			}
		}
	}
	waves, err := d.ComputeWaves()
	if err != nil {
		t.Fatalf("ComputeWaves: %v", err)
	}
	return waves, deps, titles
}

func TestRender_SingleNode(t *testing.T) {
	t.Parallel()

	waves, deps, titles := buildTestDAG(t, []dagSpec{
		{id: "alpha", title: "Alpha Phase"},
	})

	r := &DAGRenderer{Width: 80, UseColor: false}
	out := r.Render(waves, deps, titles)

	if out == "" {
		t.Fatal("Render returned empty string for single node")
	}
	if !strings.Contains(out, "Alpha Phase") {
		t.Errorf("output missing title 'Alpha Phase':\n%s", out)
	}
	// Should have box-drawing characters.
	if !strings.Contains(out, "┌") || !strings.Contains(out, "┘") {
		t.Errorf("output missing box borders:\n%s", out)
	}
}

func TestRender_TwoNodeChain(t *testing.T) {
	t.Parallel()

	waves, deps, titles := buildTestDAG(t, []dagSpec{
		{id: "a", title: "First"},
		{id: "b", title: "Second", deps: []string{"a"}},
	})

	r := &DAGRenderer{Width: 80, UseColor: false}
	out := r.Render(waves, deps, titles)

	if !strings.Contains(out, "First") {
		t.Errorf("output missing 'First':\n%s", out)
	}
	if !strings.Contains(out, "Second") {
		t.Errorf("output missing 'Second':\n%s", out)
	}
	// Should have connector characters between waves.
	if !strings.Contains(out, "│") {
		t.Errorf("output missing vertical connector '│':\n%s", out)
	}
}

func TestRender_Diamond(t *testing.T) {
	t.Parallel()

	// d (no deps) → b,c depend on d → a depends on b,c
	waves, deps, titles := buildTestDAG(t, []dagSpec{
		{id: "d", title: "Root"},
		{id: "b", title: "Left", deps: []string{"d"}},
		{id: "c", title: "Right", deps: []string{"d"}},
		{id: "a", title: "Top", deps: []string{"b", "c"}},
	})

	r := &DAGRenderer{Width: 100, UseColor: false}
	out := r.Render(waves, deps, titles)

	if !strings.Contains(out, "Root") {
		t.Errorf("output missing 'Root':\n%s", out)
	}
	if !strings.Contains(out, "Left") {
		t.Errorf("output missing 'Left':\n%s", out)
	}
	if !strings.Contains(out, "Right") {
		t.Errorf("output missing 'Right':\n%s", out)
	}
	if !strings.Contains(out, "Top") {
		t.Errorf("output missing 'Top':\n%s", out)
	}

	// Should have 3 waves.
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Errorf("expected at least 3 output lines for diamond, got %d:\n%s", len(lines), out)
	}
}

func TestRender_ThreeWayFanOut(t *testing.T) {
	t.Parallel()

	waves, deps, titles := buildTestDAG(t, []dagSpec{
		{id: "root", title: "Root"},
		{id: "a", title: "Branch A", deps: []string{"root"}},
		{id: "b", title: "Branch B", deps: []string{"root"}},
		{id: "c", title: "Branch C", deps: []string{"root"}},
	})

	r := &DAGRenderer{Width: 120, UseColor: false}
	out := r.Render(waves, deps, titles)

	for _, name := range []string{"Root", "Branch A", "Branch B", "Branch C"} {
		if !strings.Contains(out, name) {
			t.Errorf("output missing %q:\n%s", name, out)
		}
	}
}

func TestRender_CompactMode(t *testing.T) {
	t.Parallel()

	// Build 11 nodes to trigger compact mode.
	specs := make([]dagSpec, 0, 11)
	specs = append(specs, dagSpec{id: "root", title: "Root"})
	for i := 1; i <= 10; i++ {
		id := strings.Repeat(string(rune('a'+i-1)), 1)
		specs = append(specs, dagSpec{
			id:    id,
			title: "Phase " + id,
			deps:  []string{"root"},
		})
	}

	waves, deps, titles := buildTestDAG(t, specs)

	r := &DAGRenderer{Width: 120, UseColor: false}
	out := r.Render(waves, deps, titles)

	// Compact mode should use bracket notation.
	if !strings.Contains(out, "[") || !strings.Contains(out, "]") {
		t.Errorf("compact mode should use bracket notation:\n%s", out)
	}
	// Should contain wave labels.
	if !strings.Contains(out, "Wave 1") {
		t.Errorf("compact mode should contain wave labels:\n%s", out)
	}
}

func TestRender_Deterministic(t *testing.T) {
	t.Parallel()

	waves, deps, titles := buildTestDAG(t, []dagSpec{
		{id: "d", title: "Root"},
		{id: "b", title: "Left", deps: []string{"d"}},
		{id: "c", title: "Right", deps: []string{"d"}},
		{id: "a", title: "Top", deps: []string{"b", "c"}},
	})

	r := &DAGRenderer{Width: 100, UseColor: false}

	first := r.Render(waves, deps, titles)
	for i := 0; i < 10; i++ {
		got := r.Render(waves, deps, titles)
		if got != first {
			t.Fatalf("render is non-deterministic:\nfirst:\n%s\nattempt %d:\n%s", first, i, got)
		}
	}
}

func TestRender_StatusColors(t *testing.T) {
	t.Parallel()

	waves, deps, titles := buildTestDAG(t, []dagSpec{
		{id: "a", title: "Done Phase"},
		{id: "b", title: "Running Phase", deps: []string{"a"}},
	})

	r := &DAGRenderer{
		Width:    80,
		UseColor: true,
		StatusFunc: func(id string) NodeStatus {
			switch id {
			case "a":
				return NodeStatus{State: "done", Cost: 1.50, Cycles: 3}
			case "b":
				return NodeStatus{State: "running"}
			}
			return NodeStatus{State: "queued"}
		},
	}
	out := r.Render(waves, deps, titles)

	// Should contain ANSI escape sequences.
	if !strings.Contains(out, "\033[") {
		t.Errorf("colored output should contain ANSI escapes:\n%s", out)
	}
	// Green for done.
	if !strings.Contains(out, "\033[32m") {
		t.Errorf("done phase should be green:\n%s", out)
	}
	// Yellow for running.
	if !strings.Contains(out, "\033[33m") {
		t.Errorf("running phase should be yellow:\n%s", out)
	}
}

func TestRender_CriticalPath(t *testing.T) {
	t.Parallel()

	waves, deps, titles := buildTestDAG(t, []dagSpec{
		{id: "a", title: "Start"},
		{id: "b", title: "Middle", deps: []string{"a"}},
		{id: "c", title: "End", deps: []string{"b"}},
	})

	r := &DAGRenderer{
		Width:        80,
		UseColor:     true,
		CriticalPath: map[string]bool{"a": true, "b": true, "c": true},
	}
	out := r.Render(waves, deps, titles)

	// Bold escape code should appear for critical path.
	if !strings.Contains(out, "\033[1m") {
		t.Errorf("critical path should use bold:\n%s", out)
	}
}

func TestRender_CriticalPathCompact(t *testing.T) {
	t.Parallel()

	// 11 nodes to trigger compact mode; mark a few as critical.
	specs := make([]dagSpec, 0, 12)
	specs = append(specs, dagSpec{id: "root", title: "Root"})
	for i := 1; i <= 11; i++ {
		id := strings.Repeat(string(rune('a'+i-1)), 1)
		specs = append(specs, dagSpec{
			id:    id,
			title: "P" + id,
			deps:  []string{"root"},
		})
	}

	waves, deps, titles := buildTestDAG(t, specs)

	// Without color: critical path nodes get "*" suffix.
	r := &DAGRenderer{
		Width:        120,
		UseColor:     false,
		CriticalPath: map[string]bool{"root": true, "a": true},
	}
	out := r.Render(waves, deps, titles)

	if !strings.Contains(out, "*") {
		t.Errorf("compact no-color critical path should show '*':\n%s", out)
	}
}

func TestRender_TrackBorders(t *testing.T) {
	t.Parallel()

	waves, deps, titles := buildTestDAG(t, []dagSpec{
		{id: "a", title: "Track0"},
		{id: "b", title: "Track1"},
	})

	r := &DAGRenderer{
		Width:    80,
		UseColor: false,
		TrackMap: map[string]int{"a": 0, "b": 1},
	}
	out := r.Render(waves, deps, titles)

	// Track 0 uses single-line borders.
	if !strings.Contains(out, "┌") {
		t.Errorf("track 0 should have single borders:\n%s", out)
	}
	// Track 1 uses double borders.
	if !strings.Contains(out, "╔") {
		t.Errorf("track 1 should have double borders:\n%s", out)
	}
}

func TestRender_Empty(t *testing.T) {
	t.Parallel()

	r := &DAGRenderer{Width: 80, UseColor: false}
	out := r.Render(nil, nil, nil)
	if out != "" {
		t.Errorf("empty input should return empty string, got: %q", out)
	}

	out = r.Render([]dag.Wave{}, nil, nil)
	if out != "" {
		t.Errorf("empty waves should return empty string, got: %q", out)
	}
}

func TestRender_NarrowWidth(t *testing.T) {
	t.Parallel()

	waves, deps, titles := buildTestDAG(t, []dagSpec{
		{id: "a", title: "Alpha"},
		{id: "b", title: "Beta", deps: []string{"a"}},
	})

	// Minimum width should still produce output without panicking.
	r := &DAGRenderer{Width: 20, UseColor: false}
	out := r.Render(waves, deps, titles)

	if out == "" {
		t.Fatal("narrow width should still produce output")
	}
}

func TestRender_WideWidth(t *testing.T) {
	t.Parallel()

	waves, deps, titles := buildTestDAG(t, []dagSpec{
		{id: "a", title: "Alpha"},
		{id: "b", title: "Beta", deps: []string{"a"}},
	})

	r := &DAGRenderer{Width: 200, UseColor: false}
	out := r.Render(waves, deps, titles)

	if out == "" {
		t.Fatal("wide width should produce output")
	}
	if !strings.Contains(out, "Alpha") || !strings.Contains(out, "Beta") {
		t.Errorf("output should contain node titles:\n%s", out)
	}
}

func TestRender_StatusAllStates(t *testing.T) {
	t.Parallel()

	states := []string{"queued", "running", "done", "failed", "blocked"}
	for _, state := range states {
		state := state
		t.Run(state, func(t *testing.T) {
			t.Parallel()

			waves, deps, titles := buildTestDAG(t, []dagSpec{
				{id: "node", title: "Test Node"},
			})

			r := &DAGRenderer{
				Width:    80,
				UseColor: true,
				StatusFunc: func(id string) NodeStatus {
					return NodeStatus{State: state}
				},
			}
			out := r.Render(waves, deps, titles)

			if out == "" {
				t.Fatalf("state %q should produce output", state)
			}
			// All states should have ANSI codes.
			if !strings.Contains(out, "\033[") {
				t.Errorf("state %q with UseColor should contain ANSI codes:\n%s", state, out)
			}
		})
	}
}

func TestRender_LargeDAG_12Nodes(t *testing.T) {
	t.Parallel()

	// Build a multi-wave DAG with 12 nodes.
	specs := []dagSpec{
		{id: "p1", title: "Phase 1"},
		{id: "p2", title: "Phase 2"},
		{id: "p3", title: "Phase 3", deps: []string{"p1"}},
		{id: "p4", title: "Phase 4", deps: []string{"p1"}},
		{id: "p5", title: "Phase 5", deps: []string{"p2"}},
		{id: "p6", title: "Phase 6", deps: []string{"p2"}},
		{id: "p7", title: "Phase 7", deps: []string{"p3", "p4"}},
		{id: "p8", title: "Phase 8", deps: []string{"p5"}},
		{id: "p9", title: "Phase 9", deps: []string{"p6"}},
		{id: "p10", title: "Phase 10", deps: []string{"p7", "p8"}},
		{id: "p11", title: "Phase 11", deps: []string{"p9"}},
		{id: "p12", title: "Phase 12", deps: []string{"p10", "p11"}},
	}

	waves, deps, titles := buildTestDAG(t, specs)

	r := &DAGRenderer{Width: 120, UseColor: false}
	out := r.Render(waves, deps, titles)

	// Should be in compact mode (>10 nodes).
	if !strings.Contains(out, "Wave") {
		t.Errorf("12-node DAG should be in compact mode with Wave labels:\n%s", out)
	}

	// Every phase should appear.
	for _, s := range specs {
		if !strings.Contains(out, s.title) {
			t.Errorf("missing phase %q in output:\n%s", s.title, out)
		}
	}
}

func TestRender_NoColorMode(t *testing.T) {
	t.Parallel()

	waves, deps, titles := buildTestDAG(t, []dagSpec{
		{id: "a", title: "Alpha"},
	})

	r := &DAGRenderer{Width: 80, UseColor: false}
	out := r.Render(waves, deps, titles)

	if strings.Contains(out, "\033[") {
		t.Errorf("UseColor=false should not contain ANSI escapes:\n%s", out)
	}
}

func TestRender_DefaultWidth(t *testing.T) {
	t.Parallel()

	waves, deps, titles := buildTestDAG(t, []dagSpec{
		{id: "a", title: "Test"},
	})

	// Width=0 should default to 80.
	r := &DAGRenderer{Width: 0, UseColor: false}
	out := r.Render(waves, deps, titles)

	if out == "" {
		t.Fatal("default width should produce output")
	}
}

func TestRender_CostAndCycles(t *testing.T) {
	t.Parallel()

	waves, deps, titles := buildTestDAG(t, []dagSpec{
		{id: "a", title: "Expensive"},
	})

	r := &DAGRenderer{
		Width:    80,
		UseColor: false,
		StatusFunc: func(id string) NodeStatus {
			return NodeStatus{State: "done", Cost: 3.14, Cycles: 5}
		},
	}
	out := r.Render(waves, deps, titles)

	if !strings.Contains(out, "$3.14") {
		t.Errorf("output should show cost '$3.14':\n%s", out)
	}
	if !strings.Contains(out, "5 cyc") {
		t.Errorf("output should show '5 cyc':\n%s", out)
	}
}

func TestVisibleLen(t *testing.T) {
	t.Parallel()

	r := &DAGRenderer{}

	tests := []struct {
		name string
		in   string
		want int
	}{
		{"plain", "hello", 5},
		{"with reset", "\033[0mhello\033[0m", 5},
		{"with color", "\033[32mgreen\033[0m", 5},
		{"empty", "", 0},
		{"only escapes", "\033[1m\033[0m", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := r.visibleLen(tt.in)
			if got != tt.want {
				t.Errorf("visibleLen(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestContainsStr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ss   []string
		s    string
		want bool
	}{
		{"found", []string{"a", "b", "c"}, "b", true},
		{"not found", []string{"a", "b", "c"}, "d", false},
		{"empty slice", nil, "a", false},
		{"empty string", []string{""}, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := containsStr(tt.ss, tt.s)
			if got != tt.want {
				t.Errorf("containsStr(%v, %q) = %v, want %v", tt.ss, tt.s, got, tt.want)
			}
		})
	}
}
