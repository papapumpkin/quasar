package dag

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// helper builds a DAG from a list of node specs.
// Each spec is (id, priority, deps...).
type nodeSpec struct {
	id       string
	priority int
	deps     []string
}

func buildDAG(t *testing.T, specs []nodeSpec) *DAG {
	t.Helper()
	d := New()
	for _, s := range specs {
		if err := d.AddNode(s.id, s.priority); err != nil {
			t.Fatalf("AddNode(%q): %v", s.id, err)
		}
	}
	for _, s := range specs {
		for _, dep := range s.deps {
			if err := d.AddEdge(s.id, dep); err != nil {
				t.Fatalf("AddEdge(%q, %q): %v", s.id, dep, err)
			}
		}
	}
	return d
}

// validTopologicalOrder checks that every dependency appears before
// its dependent in the ordering.
func validTopologicalOrder(d *DAG, order []string) bool {
	pos := make(map[string]int, len(order))
	for i, id := range order {
		pos[id] = i
	}
	for id, deps := range d.adjacency {
		for dep := range deps {
			if pos[dep] >= pos[id] {
				return false
			}
		}
	}
	return true
}

func TestNew(t *testing.T) {
	t.Parallel()
	d := New()
	if d.Len() != 0 {
		t.Errorf("new DAG has %d nodes, want 0", d.Len())
	}
	if nodes := d.Nodes(); len(nodes) != 0 {
		t.Errorf("new DAG Nodes() = %v, want empty", nodes)
	}
}

func TestAddNode(t *testing.T) {
	t.Parallel()

	t.Run("basic add", func(t *testing.T) {
		t.Parallel()
		d := New()
		if err := d.AddNode("a", 1); err != nil {
			t.Fatalf("AddNode: %v", err)
		}
		if d.Len() != 1 {
			t.Errorf("Len() = %d, want 1", d.Len())
		}
		n := d.Node("a")
		if n == nil {
			t.Fatal("Node(a) returned nil")
		}
		if n.Priority != 1 {
			t.Errorf("Priority = %d, want 1", n.Priority)
		}
		if n.Metadata == nil {
			t.Error("Metadata is nil, want initialized map")
		}
	})

	t.Run("duplicate", func(t *testing.T) {
		t.Parallel()
		d := New()
		_ = d.AddNode("a", 1)
		err := d.AddNode("a", 2)
		if !errors.Is(err, ErrDuplicateNode) {
			t.Errorf("got %v, want ErrDuplicateNode", err)
		}
	})
}

func TestAddEdge(t *testing.T) {
	t.Parallel()

	t.Run("basic edge", func(t *testing.T) {
		t.Parallel()
		d := New()
		_ = d.AddNode("a", 1)
		_ = d.AddNode("b", 1)
		if err := d.AddEdge("a", "b"); err != nil {
			t.Fatalf("AddEdge: %v", err)
		}
	})

	t.Run("self edge", func(t *testing.T) {
		t.Parallel()
		d := New()
		_ = d.AddNode("a", 1)
		err := d.AddEdge("a", "a")
		if !errors.Is(err, ErrSelfEdge) {
			t.Errorf("got %v, want ErrSelfEdge", err)
		}
	})

	t.Run("missing from node", func(t *testing.T) {
		t.Parallel()
		d := New()
		_ = d.AddNode("b", 1)
		err := d.AddEdge("a", "b")
		if !errors.Is(err, ErrNodeNotFound) {
			t.Errorf("got %v, want ErrNodeNotFound", err)
		}
	})

	t.Run("missing to node", func(t *testing.T) {
		t.Parallel()
		d := New()
		_ = d.AddNode("a", 1)
		err := d.AddEdge("a", "b")
		if !errors.Is(err, ErrNodeNotFound) {
			t.Errorf("got %v, want ErrNodeNotFound", err)
		}
	})

	t.Run("duplicate edge is no-op", func(t *testing.T) {
		t.Parallel()
		d := New()
		_ = d.AddNode("a", 1)
		_ = d.AddNode("b", 1)
		_ = d.AddEdge("a", "b")
		if err := d.AddEdge("a", "b"); err != nil {
			t.Errorf("duplicate AddEdge returned error: %v", err)
		}
	})

	t.Run("cycle detection", func(t *testing.T) {
		t.Parallel()
		d := New()
		_ = d.AddNode("a", 1)
		_ = d.AddNode("b", 1)
		_ = d.AddNode("c", 1)
		_ = d.AddEdge("a", "b")
		_ = d.AddEdge("b", "c")
		err := d.AddEdge("c", "a")
		if !errors.Is(err, ErrCycle) {
			t.Errorf("got %v, want ErrCycle", err)
		}
	})
}

func TestRemove(t *testing.T) {
	t.Parallel()

	t.Run("remove middle node", func(t *testing.T) {
		t.Parallel()
		// a → b → c
		d := buildDAG(t, []nodeSpec{
			{"c", 1, nil},
			{"b", 1, []string{"c"}},
			{"a", 1, []string{"b"}},
		})
		if err := d.Remove("b"); err != nil {
			t.Fatalf("Remove: %v", err)
		}
		if d.Len() != 2 {
			t.Errorf("Len() = %d, want 2", d.Len())
		}
		if d.Node("b") != nil {
			t.Error("Node(b) still exists after removal")
		}
		// a should have no dependencies now.
		if len(d.adjacency["a"]) != 0 {
			t.Errorf("node a still has deps: %v", d.adjacency["a"])
		}
		// c should have no dependents now.
		if len(d.reverse["c"]) != 0 {
			t.Errorf("node c still has dependents: %v", d.reverse["c"])
		}
	})

	t.Run("remove nonexistent", func(t *testing.T) {
		t.Parallel()
		d := New()
		err := d.Remove("x")
		if !errors.Is(err, ErrNodeNotFound) {
			t.Errorf("got %v, want ErrNodeNotFound", err)
		}
	})
}

func TestTopologicalSort_Linear(t *testing.T) {
	t.Parallel()
	// a → b → c → d (a depends on b, b on c, c on d)
	d := buildDAG(t, []nodeSpec{
		{"d", 1, nil},
		{"c", 1, []string{"d"}},
		{"b", 1, []string{"c"}},
		{"a", 1, []string{"b"}},
	})
	order, err := d.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	if len(order) != 4 {
		t.Fatalf("got %d elements, want 4", len(order))
	}
	if !validTopologicalOrder(d, order) {
		t.Errorf("invalid topological order: %v", order)
	}
	// In a linear chain with equal priorities, d must come first.
	if order[0] != "d" {
		t.Errorf("expected d first, got %s", order[0])
	}
	if order[3] != "a" {
		t.Errorf("expected a last, got %s", order[3])
	}
}

func TestTopologicalSort_Diamond(t *testing.T) {
	t.Parallel()
	//     a
	//    / \
	//   b   c
	//    \ /
	//     d
	// d has no deps, b and c depend on d, a depends on b and c.
	d := buildDAG(t, []nodeSpec{
		{"d", 1, nil},
		{"b", 1, []string{"d"}},
		{"c", 1, []string{"d"}},
		{"a", 1, []string{"b", "c"}},
	})
	order, err := d.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	if len(order) != 4 {
		t.Fatalf("got %d elements, want 4", len(order))
	}
	if !validTopologicalOrder(d, order) {
		t.Errorf("invalid topological order: %v", order)
	}
}

func TestTopologicalSort_Wide(t *testing.T) {
	t.Parallel()
	// All independent nodes: a, b, c, d (no edges).
	d := buildDAG(t, []nodeSpec{
		{"a", 1, nil},
		{"b", 1, nil},
		{"c", 1, nil},
		{"d", 1, nil},
	})
	order, err := d.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	if len(order) != 4 {
		t.Fatalf("got %d elements, want 4", len(order))
	}
	if !validTopologicalOrder(d, order) {
		t.Errorf("invalid topological order: %v", order)
	}
}

func TestTopologicalSort_PriorityOrdering(t *testing.T) {
	t.Parallel()
	// Three independent nodes with different priorities.
	// Should come out in priority order: high(3), med(2), low(1).
	d := buildDAG(t, []nodeSpec{
		{"low", 1, nil},
		{"med", 2, nil},
		{"high", 3, nil},
	})
	order, err := d.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	want := []string{"high", "med", "low"}
	if len(order) != len(want) {
		t.Fatalf("got %v, want %v", order, want)
	}
	for i, id := range want {
		if order[i] != id {
			t.Errorf("order[%d] = %q, want %q (full: %v)", i, order[i], id, order)
			break
		}
	}
}

func TestTopologicalSort_PriorityWithDeps(t *testing.T) {
	t.Parallel()
	// dep (prio 1) has no deps.
	// high (prio 3) depends on dep.
	// low (prio 1) has no deps.
	// Expected: dep and low are both ready initially.
	// low has prio 1, dep has prio 1 → alphabetical: dep before low.
	// After dep: high becomes ready and has prio 3, but low still waiting.
	// Actually, low is already in queue. Let's verify the invariant.
	d := buildDAG(t, []nodeSpec{
		{"dep", 1, nil},
		{"high", 3, []string{"dep"}},
		{"low", 1, nil},
	})
	order, err := d.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	if !validTopologicalOrder(d, order) {
		t.Errorf("invalid topological order: %v", order)
	}
}

func TestTopologicalSort_SingleNode(t *testing.T) {
	t.Parallel()
	d := buildDAG(t, []nodeSpec{
		{"only", 1, nil},
	})
	order, err := d.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	if len(order) != 1 || order[0] != "only" {
		t.Errorf("got %v, want [only]", order)
	}
}

func TestTopologicalSort_Empty(t *testing.T) {
	t.Parallel()
	d := New()
	order, err := d.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	if len(order) != 0 {
		t.Errorf("got %v, want empty", order)
	}
}

func TestTopologicalSort_CycleDetection(t *testing.T) {
	t.Parallel()
	// Build manually to bypass AddEdge cycle check.
	d := New()
	_ = d.AddNode("a", 1)
	_ = d.AddNode("b", 1)
	// Force a cycle by manipulating internal state.
	d.adjacency["a"]["b"] = true
	d.reverse["b"]["a"] = true
	d.adjacency["b"]["a"] = true
	d.reverse["a"]["b"] = true

	_, err := d.TopologicalSort()
	if !errors.Is(err, ErrCycle) {
		t.Errorf("got %v, want ErrCycle", err)
	}
}

func TestTopologicalSort_ComplexDAG(t *testing.T) {
	t.Parallel()
	//   a
	//  / \
	// b   c
	// |   |
	// d   e
	//  \ /
	//   f
	d := buildDAG(t, []nodeSpec{
		{"f", 1, nil},
		{"d", 1, []string{"f"}},
		{"e", 1, []string{"f"}},
		{"b", 1, []string{"d"}},
		{"c", 1, []string{"e"}},
		{"a", 1, []string{"b", "c"}},
	})
	order, err := d.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	if len(order) != 6 {
		t.Fatalf("got %d elements, want 6", len(order))
	}
	if !validTopologicalOrder(d, order) {
		t.Errorf("invalid topological order: %v", order)
	}
}

func TestReady(t *testing.T) {
	t.Parallel()

	t.Run("no done", func(t *testing.T) {
		t.Parallel()
		// a → b → c
		d := buildDAG(t, []nodeSpec{
			{"c", 1, nil},
			{"b", 1, []string{"c"}},
			{"a", 1, []string{"b"}},
		})
		ready := d.Ready(nil)
		if len(ready) != 1 || ready[0] != "c" {
			t.Errorf("Ready(nil) = %v, want [c]", ready)
		}
	})

	t.Run("partial done", func(t *testing.T) {
		t.Parallel()
		d := buildDAG(t, []nodeSpec{
			{"c", 1, nil},
			{"b", 1, []string{"c"}},
			{"a", 1, []string{"b"}},
		})
		ready := d.Ready(map[string]bool{"c": true})
		if len(ready) != 1 || ready[0] != "b" {
			t.Errorf("Ready({c}) = %v, want [b]", ready)
		}
	})

	t.Run("all done", func(t *testing.T) {
		t.Parallel()
		d := buildDAG(t, []nodeSpec{
			{"a", 1, nil},
		})
		ready := d.Ready(map[string]bool{"a": true})
		if len(ready) != 0 {
			t.Errorf("Ready(all done) = %v, want empty", ready)
		}
	})

	t.Run("priority ordering", func(t *testing.T) {
		t.Parallel()
		// Three independent nodes with different priorities.
		d := buildDAG(t, []nodeSpec{
			{"low", 1, nil},
			{"med", 2, nil},
			{"high", 3, nil},
		})
		ready := d.Ready(nil)
		if len(ready) != 3 {
			t.Fatalf("Ready() has %d items, want 3", len(ready))
		}
		if ready[0] != "high" {
			t.Errorf("first ready = %q, want high", ready[0])
		}
		if ready[1] != "med" {
			t.Errorf("second ready = %q, want med", ready[1])
		}
		if ready[2] != "low" {
			t.Errorf("third ready = %q, want low", ready[2])
		}
	})

	t.Run("diamond partial", func(t *testing.T) {
		t.Parallel()
		d := buildDAG(t, []nodeSpec{
			{"d", 1, nil},
			{"b", 2, []string{"d"}},
			{"c", 1, []string{"d"}},
			{"a", 3, []string{"b", "c"}},
		})
		// d done: b and c should be ready.
		ready := d.Ready(map[string]bool{"d": true})
		if len(ready) != 2 {
			t.Fatalf("Ready({d}) = %v, want 2 items", ready)
		}
		// b has higher priority.
		if ready[0] != "b" {
			t.Errorf("first ready = %q, want b (higher priority)", ready[0])
		}
	})

	t.Run("empty DAG", func(t *testing.T) {
		t.Parallel()
		d := New()
		ready := d.Ready(nil)
		if len(ready) != 0 {
			t.Errorf("Ready on empty DAG = %v, want empty", ready)
		}
	})
}

func TestAncestors(t *testing.T) {
	t.Parallel()

	t.Run("linear chain", func(t *testing.T) {
		t.Parallel()
		// a → b → c → d
		d := buildDAG(t, []nodeSpec{
			{"d", 1, nil},
			{"c", 1, []string{"d"}},
			{"b", 1, []string{"c"}},
			{"a", 1, []string{"b"}},
		})
		ancestors := d.Ancestors("a")
		want := []string{"b", "c", "d"}
		if len(ancestors) != len(want) {
			t.Fatalf("Ancestors(a) = %v, want %v", ancestors, want)
		}
		for i, id := range want {
			if ancestors[i] != id {
				t.Errorf("ancestors[%d] = %q, want %q", i, ancestors[i], id)
			}
		}
	})

	t.Run("leaf node has no ancestors", func(t *testing.T) {
		t.Parallel()
		d := buildDAG(t, []nodeSpec{
			{"d", 1, nil},
			{"c", 1, []string{"d"}},
		})
		ancestors := d.Ancestors("d")
		if len(ancestors) != 0 {
			t.Errorf("Ancestors(d) = %v, want empty", ancestors)
		}
	})

	t.Run("diamond", func(t *testing.T) {
		t.Parallel()
		d := buildDAG(t, []nodeSpec{
			{"d", 1, nil},
			{"b", 1, []string{"d"}},
			{"c", 1, []string{"d"}},
			{"a", 1, []string{"b", "c"}},
		})
		ancestors := d.Ancestors("a")
		want := []string{"b", "c", "d"}
		if len(ancestors) != len(want) {
			t.Fatalf("Ancestors(a) = %v, want %v", ancestors, want)
		}
		for i, id := range want {
			if ancestors[i] != id {
				t.Errorf("ancestors[%d] = %q, want %q", i, ancestors[i], id)
			}
		}
	})

	t.Run("nonexistent node", func(t *testing.T) {
		t.Parallel()
		d := New()
		ancestors := d.Ancestors("x")
		if ancestors != nil {
			t.Errorf("Ancestors(x) = %v, want nil", ancestors)
		}
	})
}

func TestDescendants(t *testing.T) {
	t.Parallel()

	t.Run("linear chain", func(t *testing.T) {
		t.Parallel()
		// a → b → c → d
		d := buildDAG(t, []nodeSpec{
			{"d", 1, nil},
			{"c", 1, []string{"d"}},
			{"b", 1, []string{"c"}},
			{"a", 1, []string{"b"}},
		})
		desc := d.Descendants("d")
		want := []string{"a", "b", "c"}
		if len(desc) != len(want) {
			t.Fatalf("Descendants(d) = %v, want %v", desc, want)
		}
		for i, id := range want {
			if desc[i] != id {
				t.Errorf("descendants[%d] = %q, want %q", i, desc[i], id)
			}
		}
	})

	t.Run("root has no descendants", func(t *testing.T) {
		t.Parallel()
		d := buildDAG(t, []nodeSpec{
			{"b", 1, nil},
			{"a", 1, []string{"b"}},
		})
		desc := d.Descendants("a")
		if len(desc) != 0 {
			t.Errorf("Descendants(a) = %v, want empty", desc)
		}
	})

	t.Run("diamond", func(t *testing.T) {
		t.Parallel()
		d := buildDAG(t, []nodeSpec{
			{"d", 1, nil},
			{"b", 1, []string{"d"}},
			{"c", 1, []string{"d"}},
			{"a", 1, []string{"b", "c"}},
		})
		desc := d.Descendants("d")
		want := []string{"a", "b", "c"}
		if len(desc) != len(want) {
			t.Fatalf("Descendants(d) = %v, want %v", desc, want)
		}
		for i, id := range want {
			if desc[i] != id {
				t.Errorf("descendants[%d] = %q, want %q", i, desc[i], id)
			}
		}
	})

	t.Run("nonexistent node", func(t *testing.T) {
		t.Parallel()
		d := New()
		desc := d.Descendants("x")
		if desc != nil {
			t.Errorf("Descendants(x) = %v, want nil", desc)
		}
	})
}

func TestCycleDetectionOnAddEdge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(*DAG)
		from  string
		to    string
	}{
		{
			name: "direct cycle A→B→A",
			setup: func(d *DAG) {
				_ = d.AddNode("a", 1)
				_ = d.AddNode("b", 1)
				_ = d.AddEdge("a", "b")
			},
			from: "b",
			to:   "a",
		},
		{
			name: "transitive cycle A→B→C→A",
			setup: func(d *DAG) {
				_ = d.AddNode("a", 1)
				_ = d.AddNode("b", 1)
				_ = d.AddNode("c", 1)
				_ = d.AddEdge("a", "b")
				_ = d.AddEdge("b", "c")
			},
			from: "c",
			to:   "a",
		},
		{
			name: "long chain cycle",
			setup: func(d *DAG) {
				_ = d.AddNode("a", 1)
				_ = d.AddNode("b", 1)
				_ = d.AddNode("c", 1)
				_ = d.AddNode("d", 1)
				_ = d.AddNode("e", 1)
				_ = d.AddEdge("a", "b")
				_ = d.AddEdge("b", "c")
				_ = d.AddEdge("c", "d")
				_ = d.AddEdge("d", "e")
			},
			from: "e",
			to:   "a",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := New()
			tt.setup(d)
			err := d.AddEdge(tt.from, tt.to)
			if !errors.Is(err, ErrCycle) {
				t.Errorf("AddEdge(%q, %q) = %v, want ErrCycle", tt.from, tt.to, err)
			}
		})
	}
}

func TestCycleError_Message(t *testing.T) {
	t.Parallel()
	d := New()
	_ = d.AddNode("a", 1)
	_ = d.AddNode("b", 1)
	// Force cycle.
	d.adjacency["a"]["b"] = true
	d.reverse["b"]["a"] = true
	d.adjacency["b"]["a"] = true
	d.reverse["a"]["b"] = true

	_, err := d.TopologicalSort()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Errorf("error message %q does not contain 'cycle detected'", err.Error())
	}
}

func TestNodeMetadata(t *testing.T) {
	t.Parallel()
	d := New()
	_ = d.AddNode("a", 5)
	n := d.Node("a")
	n.Metadata["key"] = "value"
	if n.Metadata["key"] != "value" {
		t.Error("metadata not stored correctly")
	}
}

func TestNodeLookup_NotFound(t *testing.T) {
	t.Parallel()
	d := New()
	if n := d.Node("nonexistent"); n != nil {
		t.Errorf("Node(nonexistent) = %v, want nil", n)
	}
}

func TestNodes_Sorted(t *testing.T) {
	t.Parallel()
	d := buildDAG(t, []nodeSpec{
		{"c", 1, nil},
		{"a", 1, nil},
		{"b", 1, nil},
	})
	nodes := d.Nodes()
	want := []string{"a", "b", "c"}
	if len(nodes) != len(want) {
		t.Fatalf("Nodes() = %v, want %v", nodes, want)
	}
	for i, id := range want {
		if nodes[i] != id {
			t.Errorf("nodes[%d] = %q, want %q", i, nodes[i], id)
		}
	}
}

func TestRemove_And_Sort(t *testing.T) {
	t.Parallel()
	// a → b → c, then remove b. Remaining: a, c (disconnected).
	d := buildDAG(t, []nodeSpec{
		{"c", 1, nil},
		{"b", 1, []string{"c"}},
		{"a", 1, []string{"b"}},
	})
	_ = d.Remove("b")
	order, err := d.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort after remove: %v", err)
	}
	if len(order) != 2 {
		t.Fatalf("got %d elements, want 2", len(order))
	}
	if !validTopologicalOrder(d, order) {
		t.Errorf("invalid topological order: %v", order)
	}
}

func TestReady_WithNilDone(t *testing.T) {
	t.Parallel()
	d := buildDAG(t, []nodeSpec{
		{"a", 2, nil},
		{"b", 1, nil},
		{"c", 3, nil},
	})
	ready := d.Ready(nil)
	if len(ready) != 3 {
		t.Fatalf("Ready(nil) = %v, want 3 items", ready)
	}
	// Priority order: c(3), a(2), b(1)
	if ready[0] != "c" || ready[1] != "a" || ready[2] != "b" {
		t.Errorf("Ready(nil) = %v, want [c, a, b]", ready)
	}
}

func TestLargeDAG(t *testing.T) {
	t.Parallel()
	// Build a 100-node linear chain: n99 → n98 → ... → n0
	d := New()
	ids := make([]string, 100)
	for i := 0; i < 100; i++ {
		ids[i] = fmt.Sprintf("n%03d", i)
		_ = d.AddNode(ids[i], i) // priority = index
	}
	for i := 1; i < 100; i++ {
		_ = d.AddEdge(ids[i], ids[i-1]) // n_i depends on n_(i-1)
	}

	order, err := d.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	if len(order) != 100 {
		t.Fatalf("got %d elements, want 100", len(order))
	}
	if !validTopologicalOrder(d, order) {
		t.Error("invalid topological order")
	}
	// First should be n000 (no deps), last should be n099.
	if order[0] != "n000" {
		t.Errorf("first = %q, want n000", order[0])
	}
	if order[99] != "n099" {
		t.Errorf("last = %q, want n099", order[99])
	}
}

func TestHasPath(t *testing.T) {
	t.Parallel()

	t.Run("direct path", func(t *testing.T) {
		t.Parallel()
		// a → b
		d := buildDAG(t, []nodeSpec{
			{"b", 1, nil},
			{"a", 1, []string{"b"}},
		})
		if !d.HasPath("a", "b") {
			t.Error("HasPath(a, b) = false, want true")
		}
	})

	t.Run("transitive path", func(t *testing.T) {
		t.Parallel()
		// a → b → c
		d := buildDAG(t, []nodeSpec{
			{"c", 1, nil},
			{"b", 1, []string{"c"}},
			{"a", 1, []string{"b"}},
		})
		if !d.HasPath("a", "c") {
			t.Error("HasPath(a, c) = false, want true")
		}
	})

	t.Run("no path", func(t *testing.T) {
		t.Parallel()
		// a → b, c (disconnected)
		d := buildDAG(t, []nodeSpec{
			{"b", 1, nil},
			{"c", 1, nil},
			{"a", 1, []string{"b"}},
		})
		if d.HasPath("a", "c") {
			t.Error("HasPath(a, c) = true, want false")
		}
	})

	t.Run("reverse direction has no path", func(t *testing.T) {
		t.Parallel()
		// a → b
		d := buildDAG(t, []nodeSpec{
			{"b", 1, nil},
			{"a", 1, []string{"b"}},
		})
		if d.HasPath("b", "a") {
			t.Error("HasPath(b, a) = true, want false")
		}
	})

	t.Run("same node returns false", func(t *testing.T) {
		t.Parallel()
		d := buildDAG(t, []nodeSpec{
			{"a", 1, nil},
		})
		if d.HasPath("a", "a") {
			t.Error("HasPath(a, a) = true, want false")
		}
	})

	t.Run("nonexistent nodes", func(t *testing.T) {
		t.Parallel()
		d := New()
		if d.HasPath("x", "y") {
			t.Error("HasPath on empty DAG = true, want false")
		}
	})

	t.Run("cycle check in AddEdge still works", func(t *testing.T) {
		t.Parallel()
		d := New()
		_ = d.AddNode("a", 1)
		_ = d.AddNode("b", 1)
		_ = d.AddEdge("a", "b")
		err := d.AddEdge("b", "a")
		if !errors.Is(err, ErrCycle) {
			t.Errorf("AddEdge(b, a) = %v, want ErrCycle", err)
		}
	})
}

func TestConnected(t *testing.T) {
	t.Parallel()

	t.Run("forward path", func(t *testing.T) {
		t.Parallel()
		// a → b
		d := buildDAG(t, []nodeSpec{
			{"b", 1, nil},
			{"a", 1, []string{"b"}},
		})
		if !d.Connected("a", "b") {
			t.Error("Connected(a, b) = false, want true")
		}
	})

	t.Run("reverse path", func(t *testing.T) {
		t.Parallel()
		// a → b; checking Connected(b, a) should be true
		d := buildDAG(t, []nodeSpec{
			{"b", 1, nil},
			{"a", 1, []string{"b"}},
		})
		if !d.Connected("b", "a") {
			t.Error("Connected(b, a) = false, want true")
		}
	})

	t.Run("disconnected", func(t *testing.T) {
		t.Parallel()
		d := buildDAG(t, []nodeSpec{
			{"a", 1, nil},
			{"b", 1, nil},
		})
		if d.Connected("a", "b") {
			t.Error("Connected(a, b) = true, want false")
		}
	})

	t.Run("transitive connection", func(t *testing.T) {
		t.Parallel()
		// a → b → c
		d := buildDAG(t, []nodeSpec{
			{"c", 1, nil},
			{"b", 1, []string{"c"}},
			{"a", 1, []string{"b"}},
		})
		if !d.Connected("c", "a") {
			t.Error("Connected(c, a) = false, want true")
		}
	})
}

func TestComputeWaves(t *testing.T) {
	t.Parallel()

	t.Run("empty DAG", func(t *testing.T) {
		t.Parallel()
		d := New()
		waves, err := d.ComputeWaves()
		if err != nil {
			t.Fatalf("ComputeWaves: %v", err)
		}
		if waves != nil {
			t.Errorf("ComputeWaves on empty DAG = %v, want nil", waves)
		}
	})

	t.Run("single node", func(t *testing.T) {
		t.Parallel()
		d := buildDAG(t, []nodeSpec{
			{"a", 1, nil},
		})
		waves, err := d.ComputeWaves()
		if err != nil {
			t.Fatalf("ComputeWaves: %v", err)
		}
		if len(waves) != 1 {
			t.Fatalf("got %d waves, want 1", len(waves))
		}
		if waves[0].Number != 0 {
			t.Errorf("wave number = %d, want 0", waves[0].Number)
		}
		if len(waves[0].NodeIDs) != 1 || waves[0].NodeIDs[0] != "a" {
			t.Errorf("wave 0 nodes = %v, want [a]", waves[0].NodeIDs)
		}
	})

	t.Run("all independent", func(t *testing.T) {
		t.Parallel()
		d := buildDAG(t, []nodeSpec{
			{"c", 1, nil},
			{"a", 1, nil},
			{"b", 1, nil},
		})
		waves, err := d.ComputeWaves()
		if err != nil {
			t.Fatalf("ComputeWaves: %v", err)
		}
		if len(waves) != 1 {
			t.Fatalf("got %d waves, want 1", len(waves))
		}
		want := []string{"a", "b", "c"}
		if len(waves[0].NodeIDs) != len(want) {
			t.Fatalf("wave 0 nodes = %v, want %v", waves[0].NodeIDs, want)
		}
		for i, id := range want {
			if waves[0].NodeIDs[i] != id {
				t.Errorf("wave 0 node[%d] = %q, want %q", i, waves[0].NodeIDs[i], id)
			}
		}
	})

	t.Run("linear chain", func(t *testing.T) {
		t.Parallel()
		// a → b → c (a depends on b, b depends on c)
		d := buildDAG(t, []nodeSpec{
			{"c", 1, nil},
			{"b", 1, []string{"c"}},
			{"a", 1, []string{"b"}},
		})
		waves, err := d.ComputeWaves()
		if err != nil {
			t.Fatalf("ComputeWaves: %v", err)
		}
		if len(waves) != 3 {
			t.Fatalf("got %d waves, want 3", len(waves))
		}
		// Wave 0: c (no deps), Wave 1: b (depends on c), Wave 2: a (depends on b)
		wantWaves := [][]string{{"c"}, {"b"}, {"a"}}
		for i, ww := range wantWaves {
			if waves[i].Number != i {
				t.Errorf("wave %d number = %d", i, waves[i].Number)
			}
			if len(waves[i].NodeIDs) != len(ww) {
				t.Errorf("wave %d = %v, want %v", i, waves[i].NodeIDs, ww)
				continue
			}
			for j, id := range ww {
				if waves[i].NodeIDs[j] != id {
					t.Errorf("wave %d node[%d] = %q, want %q", i, j, waves[i].NodeIDs[j], id)
				}
			}
		}
	})

	t.Run("diamond", func(t *testing.T) {
		t.Parallel()
		//     a
		//    / \
		//   b   c
		//    \ /
		//     d
		d := buildDAG(t, []nodeSpec{
			{"d", 1, nil},
			{"b", 1, []string{"d"}},
			{"c", 1, []string{"d"}},
			{"a", 1, []string{"b", "c"}},
		})
		waves, err := d.ComputeWaves()
		if err != nil {
			t.Fatalf("ComputeWaves: %v", err)
		}
		if len(waves) != 3 {
			t.Fatalf("got %d waves, want 3", len(waves))
		}
		// Wave 0: d, Wave 1: b,c (sorted), Wave 2: a
		wantWaves := [][]string{{"d"}, {"b", "c"}, {"a"}}
		for i, ww := range wantWaves {
			if len(waves[i].NodeIDs) != len(ww) {
				t.Errorf("wave %d = %v, want %v", i, waves[i].NodeIDs, ww)
				continue
			}
			for j, id := range ww {
				if waves[i].NodeIDs[j] != id {
					t.Errorf("wave %d node[%d] = %q, want %q", i, j, waves[i].NodeIDs[j], id)
				}
			}
		}
	})

	t.Run("cycle detection", func(t *testing.T) {
		t.Parallel()
		// Force a cycle by manipulating internal state.
		d := New()
		_ = d.AddNode("a", 1)
		_ = d.AddNode("b", 1)
		d.adjacency["a"]["b"] = true
		d.reverse["b"]["a"] = true
		d.adjacency["b"]["a"] = true
		d.reverse["a"]["b"] = true

		_, err := d.ComputeWaves()
		if !errors.Is(err, ErrCycle) {
			t.Errorf("ComputeWaves on cycle = %v, want ErrCycle", err)
		}
	})

	t.Run("complex multi-wave", func(t *testing.T) {
		t.Parallel()
		//   a
		//  / \
		// b   c
		// |   |
		// d   e
		//  \ /
		//   f
		d := buildDAG(t, []nodeSpec{
			{"f", 1, nil},
			{"d", 1, []string{"f"}},
			{"e", 1, []string{"f"}},
			{"b", 1, []string{"d"}},
			{"c", 1, []string{"e"}},
			{"a", 1, []string{"b", "c"}},
		})
		waves, err := d.ComputeWaves()
		if err != nil {
			t.Fatalf("ComputeWaves: %v", err)
		}
		if len(waves) != 4 {
			t.Fatalf("got %d waves, want 4", len(waves))
		}
		// Wave 0: f, Wave 1: d,e, Wave 2: b,c, Wave 3: a
		wantWaves := [][]string{{"f"}, {"d", "e"}, {"b", "c"}, {"a"}}
		for i, ww := range wantWaves {
			if len(waves[i].NodeIDs) != len(ww) {
				t.Errorf("wave %d = %v, want %v", i, waves[i].NodeIDs, ww)
				continue
			}
			for j, id := range ww {
				if waves[i].NodeIDs[j] != id {
					t.Errorf("wave %d node[%d] = %q, want %q", i, j, waves[i].NodeIDs[j], id)
				}
			}
		}
	})
}

func TestDepsFor(t *testing.T) {
	t.Parallel()

	t.Run("has deps", func(t *testing.T) {
		t.Parallel()
		d := buildDAG(t, []nodeSpec{
			{"c", 1, nil},
			{"b", 1, nil},
			{"a", 1, []string{"b", "c"}},
		})
		deps := d.DepsFor("a")
		want := []string{"b", "c"}
		if len(deps) != len(want) {
			t.Fatalf("DepsFor(a) = %v, want %v", deps, want)
		}
		for i, id := range want {
			if deps[i] != id {
				t.Errorf("deps[%d] = %q, want %q", i, deps[i], id)
			}
		}
	})

	t.Run("no deps", func(t *testing.T) {
		t.Parallel()
		d := buildDAG(t, []nodeSpec{
			{"a", 1, nil},
		})
		deps := d.DepsFor("a")
		if deps != nil {
			t.Errorf("DepsFor(a) = %v, want nil", deps)
		}
	})

	t.Run("nonexistent node", func(t *testing.T) {
		t.Parallel()
		d := New()
		deps := d.DepsFor("x")
		if deps != nil {
			t.Errorf("DepsFor(x) = %v, want nil", deps)
		}
	})

	t.Run("sorted alphabetically", func(t *testing.T) {
		t.Parallel()
		d := buildDAG(t, []nodeSpec{
			{"z", 1, nil},
			{"m", 1, nil},
			{"a", 1, nil},
			{"root", 1, []string{"z", "m", "a"}},
		})
		deps := d.DepsFor("root")
		want := []string{"a", "m", "z"}
		if len(deps) != len(want) {
			t.Fatalf("DepsFor(root) = %v, want %v", deps, want)
		}
		for i, id := range want {
			if deps[i] != id {
				t.Errorf("deps[%d] = %q, want %q", i, deps[i], id)
			}
		}
	})
}

func TestAddNodeIdempotent(t *testing.T) {
	t.Parallel()

	t.Run("new node", func(t *testing.T) {
		t.Parallel()
		d := New()
		d.AddNodeIdempotent("a", 5)
		if d.Len() != 1 {
			t.Errorf("Len() = %d, want 1", d.Len())
		}
		n := d.Node("a")
		if n == nil {
			t.Fatal("Node(a) returned nil")
		}
		if n.Priority != 5 {
			t.Errorf("Priority = %d, want 5", n.Priority)
		}
		if n.Metadata == nil {
			t.Error("Metadata is nil, want initialized map")
		}
	})

	t.Run("existing node is no-op", func(t *testing.T) {
		t.Parallel()
		d := New()
		d.AddNodeIdempotent("a", 5)
		d.AddNodeIdempotent("a", 10) // should be no-op
		if d.Len() != 1 {
			t.Errorf("Len() = %d, want 1", d.Len())
		}
		n := d.Node("a")
		if n.Priority != 5 {
			t.Errorf("Priority = %d, want 5 (original, not overwritten)", n.Priority)
		}
	})

	t.Run("can add edges after idempotent add", func(t *testing.T) {
		t.Parallel()
		d := New()
		d.AddNodeIdempotent("a", 1)
		d.AddNodeIdempotent("b", 1)
		if err := d.AddEdge("a", "b"); err != nil {
			t.Fatalf("AddEdge after AddNodeIdempotent: %v", err)
		}
	})
}

func TestRemoveEdge(t *testing.T) {
	t.Parallel()

	t.Run("remove existing edge", func(t *testing.T) {
		t.Parallel()
		// a → b → c
		d := buildDAG(t, []nodeSpec{
			{"c", 1, nil},
			{"b", 1, []string{"c"}},
			{"a", 1, []string{"b"}},
		})
		d.RemoveEdge("a", "b")
		// a should have no deps now.
		deps := d.DepsFor("a")
		if deps != nil {
			t.Errorf("DepsFor(a) after RemoveEdge = %v, want nil", deps)
		}
		// b→c should still exist.
		deps = d.DepsFor("b")
		if len(deps) != 1 || deps[0] != "c" {
			t.Errorf("DepsFor(b) = %v, want [c]", deps)
		}
		// Nodes should still exist.
		if d.Len() != 3 {
			t.Errorf("Len() = %d, want 3", d.Len())
		}
	})

	t.Run("remove nonexistent edge is no-op", func(t *testing.T) {
		t.Parallel()
		d := buildDAG(t, []nodeSpec{
			{"a", 1, nil},
			{"b", 1, nil},
		})
		d.RemoveEdge("a", "b") // no edge exists, should not panic
		if d.Len() != 2 {
			t.Errorf("Len() = %d, want 2", d.Len())
		}
	})

	t.Run("remove edge with nonexistent nodes is no-op", func(t *testing.T) {
		t.Parallel()
		d := New()
		d.RemoveEdge("x", "y") // no nodes exist, should not panic
	})

	t.Run("reverse map cleaned up", func(t *testing.T) {
		t.Parallel()
		d := buildDAG(t, []nodeSpec{
			{"b", 1, nil},
			{"a", 1, []string{"b"}},
		})
		d.RemoveEdge("a", "b")
		// After removing a→b, b's reverse map should no longer contain a.
		desc := d.Descendants("b")
		if len(desc) != 0 {
			t.Errorf("Descendants(b) after RemoveEdge = %v, want empty", desc)
		}
	})

	t.Run("can re-add edge after removal", func(t *testing.T) {
		t.Parallel()
		d := buildDAG(t, []nodeSpec{
			{"b", 1, nil},
			{"a", 1, []string{"b"}},
		})
		d.RemoveEdge("a", "b")
		if err := d.AddEdge("a", "b"); err != nil {
			t.Fatalf("AddEdge after RemoveEdge: %v", err)
		}
		deps := d.DepsFor("a")
		if len(deps) != 1 || deps[0] != "b" {
			t.Errorf("DepsFor(a) after re-add = %v, want [b]", deps)
		}
	})

	t.Run("allows previously cyclic edge after removal", func(t *testing.T) {
		t.Parallel()
		// a → b → c, removing b→c should allow c→a
		d := buildDAG(t, []nodeSpec{
			{"c", 1, nil},
			{"b", 1, []string{"c"}},
			{"a", 1, []string{"b"}},
		})
		d.RemoveEdge("b", "c")
		// Now there's no path from c to a, so c→a should be allowed
		if err := d.AddEdge("c", "a"); err != nil {
			t.Fatalf("AddEdge(c, a) after breaking chain: %v", err)
		}
	})
}
