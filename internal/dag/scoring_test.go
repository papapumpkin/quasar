package dag

import (
	"errors"
	"math"
	"testing"
)

// --- Test fixtures ---

// buildChain creates A → B → C → D (A depends on B, B on C, C on D).
func buildChain(t *testing.T) *DAG {
	t.Helper()
	d := New()
	for _, id := range []string{"A", "B", "C", "D"} {
		if err := d.AddNode(id, 0); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range [][2]string{{"A", "B"}, {"B", "C"}, {"C", "D"}} {
		if err := d.AddEdge(e[0], e[1]); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

// buildDiamond creates:
//
//	A → B → D
//	A → C → D
func buildDiamond(t *testing.T) *DAG {
	t.Helper()
	d := New()
	for _, id := range []string{"A", "B", "C", "D"} {
		if err := d.AddNode(id, 0); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range [][2]string{{"A", "B"}, {"A", "C"}, {"B", "D"}, {"C", "D"}} {
		if err := d.AddEdge(e[0], e[1]); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

// buildFanIn creates A, B, C all depending on D (star topology).
func buildFanIn(t *testing.T) *DAG {
	t.Helper()
	d := New()
	for _, id := range []string{"A", "B", "C", "D"} {
		if err := d.AddNode(id, 0); err != nil {
			t.Fatal(err)
		}
	}
	for _, from := range []string{"A", "B", "C"} {
		if err := d.AddEdge(from, "D"); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

// buildBottleneck creates:
//
//	A → C → D
//	B → C → D
//
// C is the bottleneck node.
func buildBottleneck(t *testing.T) *DAG {
	t.Helper()
	d := New()
	for _, id := range []string{"A", "B", "C", "D"} {
		if err := d.AddNode(id, 0); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range [][2]string{{"A", "C"}, {"B", "C"}, {"C", "D"}} {
		if err := d.AddEdge(e[0], e[1]); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

// buildComplex creates a more complex DAG:
//
//	A → C → E
//	B → C → E
//	B → D → E
//
// C and D are intermediate; C is depended on by both A and B.
func buildComplex(t *testing.T) *DAG {
	t.Helper()
	d := New()
	for _, id := range []string{"A", "B", "C", "D", "E"} {
		if err := d.AddNode(id, 0); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range [][2]string{
		{"A", "C"}, {"B", "C"}, {"B", "D"}, {"C", "E"}, {"D", "E"},
	} {
		if err := d.AddEdge(e[0], e[1]); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

const floatTol = 1e-4

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < floatTol
}

// --- PageRank tests ---

func TestPageRank_Empty(t *testing.T) {
	t.Parallel()
	d := New()
	pr := d.PageRank(DefaultPageRankOptions())
	if len(pr) != 0 {
		t.Errorf("expected empty map, got %d entries", len(pr))
	}
}

func TestPageRank_SingleNode(t *testing.T) {
	t.Parallel()
	d := New()
	if err := d.AddNode("X", 0); err != nil {
		t.Fatal(err)
	}
	pr := d.PageRank(DefaultPageRankOptions())
	if !approxEqual(pr["X"], 1.0) {
		t.Errorf("single node PageRank = %f, want ~1.0", pr["X"])
	}
}

func TestPageRank_Chain(t *testing.T) {
	t.Parallel()
	d := buildChain(t)
	pr := d.PageRank(DefaultPageRankOptions())

	// D is the root dependency (no deps itself, everything depends on it
	// transitively). It should have the highest PageRank.
	// A has no dependents → lowest PageRank.
	if pr["D"] <= pr["C"] {
		t.Errorf("expected PR[D] > PR[C], got %f <= %f", pr["D"], pr["C"])
	}
	if pr["C"] <= pr["B"] {
		t.Errorf("expected PR[C] > PR[B], got %f <= %f", pr["C"], pr["B"])
	}
	if pr["B"] <= pr["A"] {
		t.Errorf("expected PR[B] > PR[A], got %f <= %f", pr["B"], pr["A"])
	}
}

func TestPageRank_Diamond(t *testing.T) {
	t.Parallel()
	d := buildDiamond(t)
	pr := d.PageRank(DefaultPageRankOptions())

	// D is depended on by both B and C → highest.
	// B and C are symmetric → equal.
	// A has no dependents → lowest.
	if pr["D"] <= pr["B"] || pr["D"] <= pr["C"] {
		t.Errorf("expected PR[D] > PR[B],PR[C]; got D=%f B=%f C=%f",
			pr["D"], pr["B"], pr["C"])
	}
	if !approxEqual(pr["B"], pr["C"]) {
		t.Errorf("expected PR[B] == PR[C], got %f vs %f", pr["B"], pr["C"])
	}
	if pr["B"] <= pr["A"] {
		t.Errorf("expected PR[B] > PR[A], got %f <= %f", pr["B"], pr["A"])
	}
}

func TestPageRank_FanIn(t *testing.T) {
	t.Parallel()
	d := buildFanIn(t)
	pr := d.PageRank(DefaultPageRankOptions())

	// D is depended on by A, B, and C → highest by far.
	// A, B, C are symmetric → equal.
	if pr["D"] <= pr["A"] {
		t.Errorf("expected PR[D] > PR[A], got %f <= %f", pr["D"], pr["A"])
	}
	if !approxEqual(pr["A"], pr["B"]) || !approxEqual(pr["B"], pr["C"]) {
		t.Errorf("expected PR[A]==PR[B]==PR[C], got A=%f B=%f C=%f",
			pr["A"], pr["B"], pr["C"])
	}
}

func TestPageRank_SumsToOne(t *testing.T) {
	t.Parallel()
	d := buildComplex(t)
	pr := d.PageRank(DefaultPageRankOptions())

	var total float64
	for _, v := range pr {
		total += v
	}
	if !approxEqual(total, 1.0) {
		t.Errorf("PageRank sum = %f, want ~1.0", total)
	}
}

func TestPageRank_Convergence(t *testing.T) {
	t.Parallel()
	// Build a larger DAG (20-node chain) to test convergence.
	d := New()
	ids := make([]string, 20)
	for i := range ids {
		ids[i] = string(rune('a' + i))
		if err := d.AddNode(ids[i], 0); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < len(ids)-1; i++ {
		if err := d.AddEdge(ids[i], ids[i+1]); err != nil {
			t.Fatal(err)
		}
	}

	pr := d.PageRank(DefaultPageRankOptions())

	// Verify monotonic ordering: later nodes (deeper deps) get higher rank.
	for i := 0; i < len(ids)-1; i++ {
		if pr[ids[i]] >= pr[ids[i+1]] {
			t.Errorf("expected PR[%s] < PR[%s], got %f >= %f",
				ids[i], ids[i+1], pr[ids[i]], pr[ids[i+1]])
		}
	}
}

// --- Betweenness Centrality tests ---

func TestBetweenness_Empty(t *testing.T) {
	t.Parallel()
	d := New()
	bc := d.BetweennessCentrality()
	if len(bc) != 0 {
		t.Errorf("expected empty map, got %d entries", len(bc))
	}
}

func TestBetweenness_TwoNodes(t *testing.T) {
	t.Parallel()
	d := New()
	if err := d.AddNode("A", 0); err != nil {
		t.Fatal(err)
	}
	if err := d.AddNode("B", 0); err != nil {
		t.Fatal(err)
	}
	if err := d.AddEdge("A", "B"); err != nil {
		t.Fatal(err)
	}
	bc := d.BetweennessCentrality()
	if bc["A"] != 0 || bc["B"] != 0 {
		t.Errorf("expected zero betweenness for 2-node graph, got A=%f B=%f",
			bc["A"], bc["B"])
	}
}

func TestBetweenness_Chain(t *testing.T) {
	t.Parallel()
	d := buildChain(t)
	bc := d.BetweennessCentrality()

	// In a 4-node chain D→C→B→A (execution order), B and C are both
	// intermediate nodes on shortest paths. They should have equal
	// betweenness and positive scores.
	if bc["B"] <= 0 {
		t.Errorf("expected BC[B] > 0, got %f", bc["B"])
	}
	if !approxEqual(bc["B"], bc["C"]) {
		t.Errorf("expected BC[B] == BC[C] in chain, got %f vs %f",
			bc["B"], bc["C"])
	}
	// Endpoints should have zero betweenness.
	if bc["A"] != 0 {
		t.Errorf("expected BC[A] == 0, got %f", bc["A"])
	}
	if bc["D"] != 0 {
		t.Errorf("expected BC[D] == 0, got %f", bc["D"])
	}
}

func TestBetweenness_Diamond(t *testing.T) {
	t.Parallel()
	d := buildDiamond(t)
	bc := d.BetweennessCentrality()

	// B and C are symmetric in the diamond → equal betweenness.
	if !approxEqual(bc["B"], bc["C"]) {
		t.Errorf("expected BC[B] == BC[C], got %f vs %f", bc["B"], bc["C"])
	}
	// Endpoints D and A should have zero betweenness.
	if bc["D"] != 0 {
		t.Errorf("expected BC[D] == 0, got %f", bc["D"])
	}
	if bc["A"] != 0 {
		t.Errorf("expected BC[A] == 0, got %f", bc["A"])
	}
}

func TestBetweenness_FanIn(t *testing.T) {
	t.Parallel()
	d := buildFanIn(t)
	bc := d.BetweennessCentrality()

	// No intermediate nodes in a star topology → all zeros.
	for id, score := range bc {
		if score != 0 {
			t.Errorf("expected BC[%s] == 0, got %f", id, score)
		}
	}
}

func TestBetweenness_Bottleneck(t *testing.T) {
	t.Parallel()
	d := buildBottleneck(t)
	bc := d.BetweennessCentrality()

	// C is the bottleneck: it sits on all paths from D to A and D to B.
	if bc["C"] <= 0 {
		t.Errorf("expected BC[C] > 0, got %f", bc["C"])
	}
	// C should have higher betweenness than any other node.
	for _, id := range []string{"A", "B", "D"} {
		if bc["C"] <= bc[id] {
			t.Errorf("expected BC[C] > BC[%s], got %f <= %f",
				id, bc["C"], bc[id])
		}
	}
}

func TestBetweenness_Normalized(t *testing.T) {
	t.Parallel()
	d := buildComplex(t)
	bc := d.BetweennessCentrality()

	// All scores should be in [0, 1].
	for id, score := range bc {
		if score < 0 || score > 1+floatTol {
			t.Errorf("BC[%s] = %f, outside [0, 1]", id, score)
		}
	}
}

// --- Composite scoring tests ---

func TestComputeImpact_Empty(t *testing.T) {
	t.Parallel()
	d := New()
	if err := d.ComputeImpact(DefaultScoringOptions()); err != nil {
		t.Fatal(err)
	}
}

func TestComputeImpact_SingleNode(t *testing.T) {
	t.Parallel()
	d := New()
	if err := d.AddNode("X", 0); err != nil {
		t.Fatal(err)
	}
	opts := DefaultScoringOptions()
	if err := d.ComputeImpact(opts); err != nil {
		t.Fatal(err)
	}

	// Single node: normalized PR = 1.0, betweenness = 0.
	// Impact = 0.6 * 1.0 + 0.4 * 0.0 = 0.6.
	want := opts.Alpha * 1.0
	if !approxEqual(d.Node("X").Impact, want) {
		t.Errorf("Impact = %f, want %f", d.Node("X").Impact, want)
	}
}

func TestComputeImpact_PopulatesAllNodes(t *testing.T) {
	t.Parallel()
	d := buildComplex(t)
	if err := d.ComputeImpact(DefaultScoringOptions()); err != nil {
		t.Fatal(err)
	}

	for _, id := range d.Nodes() {
		node := d.Node(id)
		if node.Impact < 0 {
			t.Errorf("Impact[%s] = %f, expected non-negative", id, node.Impact)
		}
	}
}

func TestComputeImpact_Diamond_Ordering(t *testing.T) {
	t.Parallel()
	d := buildDiamond(t)
	if err := d.ComputeImpact(DefaultScoringOptions()); err != nil {
		t.Fatal(err)
	}

	impactD := d.Node("D").Impact
	impactB := d.Node("B").Impact
	impactC := d.Node("C").Impact
	impactA := d.Node("A").Impact

	// D should have highest impact (highest PageRank as root dependency).
	if impactD <= impactB {
		t.Errorf("expected Impact[D] > Impact[B], got %f <= %f", impactD, impactB)
	}
	// B and C should be approximately equal (symmetric).
	if !approxEqual(impactB, impactC) {
		t.Errorf("expected Impact[B] == Impact[C], got %f vs %f", impactB, impactC)
	}
	// A should have the lowest impact.
	if impactA >= impactB {
		t.Errorf("expected Impact[A] < Impact[B], got %f >= %f", impactA, impactB)
	}
}

func TestComputeImpact_AlphaWeighting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		alpha float64
	}{
		{"all PageRank", 1.0},
		{"all betweenness", 0.0},
		{"balanced", 0.5},
		{"default", 0.6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := buildBottleneck(t)
			opts := DefaultScoringOptions()
			opts.Alpha = tc.alpha
			if err := d.ComputeImpact(opts); err != nil {
				t.Fatal(err)
			}

			// Every node should have a non-negative impact.
			for _, id := range d.Nodes() {
				if d.Node(id).Impact < -floatTol {
					t.Errorf("alpha=%f: Impact[%s] = %f, expected non-negative",
						tc.alpha, id, d.Node(id).Impact)
				}
			}
		})
	}
}

func TestComputeImpact_AlphaOutOfRange(t *testing.T) {
	t.Parallel()
	d := buildChain(t)

	tests := []struct {
		name  string
		alpha float64
	}{
		{"negative", -0.1},
		{"above one", 1.1},
		{"large negative", -5.0},
		{"large positive", 10.0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := DefaultScoringOptions()
			opts.Alpha = tc.alpha
			err := d.ComputeImpact(opts)
			if !errors.Is(err, ErrAlphaOutOfRange) {
				t.Errorf("alpha=%f: got err=%v, want ErrAlphaOutOfRange", tc.alpha, err)
			}
		})
	}
}

func TestComputeImpact_Bottleneck_HighScore(t *testing.T) {
	t.Parallel()
	d := buildBottleneck(t)

	// With balanced weighting, C (the bottleneck) should score highest
	// or near-highest because it has both high PageRank and high betweenness.
	opts := DefaultScoringOptions()
	opts.Alpha = 0.5 // equal weight to both signals
	if err := d.ComputeImpact(opts); err != nil {
		t.Fatal(err)
	}

	impactC := d.Node("C").Impact
	for _, id := range []string{"A", "B"} {
		if impactC <= d.Node(id).Impact {
			t.Errorf("expected Impact[C] > Impact[%s], got %f <= %f",
				id, impactC, d.Node(id).Impact)
		}
	}
}

func TestComputeImpact_Complex_RelativeOrdering(t *testing.T) {
	t.Parallel()
	d := buildComplex(t)
	if err := d.ComputeImpact(DefaultScoringOptions()); err != nil {
		t.Fatal(err)
	}

	// E is the root dependency → should have highest impact.
	impactE := d.Node("E").Impact
	for _, id := range []string{"A", "B", "C", "D"} {
		if impactE <= d.Node(id).Impact {
			t.Errorf("expected Impact[E] > Impact[%s], got %f <= %f",
				id, impactE, d.Node(id).Impact)
		}
	}

	// C is depended on by both A and B and is a bottleneck → should
	// score higher than D (only depended on by B).
	if d.Node("C").Impact <= d.Node("D").Impact {
		t.Errorf("expected Impact[C] > Impact[D], got %f <= %f",
			d.Node("C").Impact, d.Node("D").Impact)
	}
}
