package dag

import (
	"sort"
	"testing"
)

// --- UnionFind unit tests ---

func TestUnionFind_Singleton(t *testing.T) {
	t.Parallel()
	uf := NewUnionFind()
	uf.Add("a")
	uf.Add("b")

	if uf.Connected("a", "b") {
		t.Error("a and b should not be connected")
	}

	comps := uf.Components()
	if len(comps) != 2 {
		t.Errorf("Components() has %d groups, want 2", len(comps))
	}
}

func TestUnionFind_Union(t *testing.T) {
	t.Parallel()
	uf := NewUnionFind()
	uf.Add("a")
	uf.Add("b")
	uf.Add("c")

	uf.Union("a", "b")
	if !uf.Connected("a", "b") {
		t.Error("a and b should be connected after union")
	}
	if uf.Connected("a", "c") {
		t.Error("a and c should not be connected")
	}

	uf.Union("b", "c")
	if !uf.Connected("a", "c") {
		t.Error("a and c should be connected transitively")
	}

	comps := uf.Components()
	if len(comps) != 1 {
		t.Errorf("Components() has %d groups, want 1", len(comps))
	}
}

func TestUnionFind_AutoAdd(t *testing.T) {
	t.Parallel()
	uf := NewUnionFind()

	// Find auto-adds.
	root := uf.Find("x")
	if root != "x" {
		t.Errorf("Find(x) = %q, want x", root)
	}

	// Union auto-adds both.
	uf.Union("y", "z")
	if !uf.Connected("y", "z") {
		t.Error("y and z should be connected after union")
	}
}

func TestUnionFind_PathCompression(t *testing.T) {
	t.Parallel()
	uf := NewUnionFind()
	// Build a chain: a -> b -> c -> d (parents).
	for _, id := range []string{"a", "b", "c", "d"} {
		uf.Add(id)
	}
	uf.Union("a", "b")
	uf.Union("b", "c")
	uf.Union("c", "d")

	// After Find, path should be compressed — all should share root.
	root := uf.Find("a")
	for _, id := range []string{"b", "c", "d"} {
		if uf.Find(id) != root {
			t.Errorf("Find(%q) = %q, want %q (path compression)", id, uf.Find(id), root)
		}
	}
}

func TestUnionFind_IdempotentUnion(t *testing.T) {
	t.Parallel()
	uf := NewUnionFind()
	uf.Add("a")
	uf.Add("b")
	uf.Union("a", "b")
	uf.Union("a", "b") // repeat
	uf.Union("b", "a") // reverse

	comps := uf.Components()
	if len(comps) != 1 {
		t.Errorf("Components() has %d groups, want 1", len(comps))
	}
}

func TestUnionFind_ManyComponents(t *testing.T) {
	t.Parallel()
	uf := NewUnionFind()
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		uf.Add(id)
	}
	uf.Union("a", "b")
	uf.Union("d", "e")

	comps := uf.Components()
	if len(comps) != 3 {
		t.Errorf("Components() has %d groups, want 3 ({a,b}, {c}, {d,e})", len(comps))
	}

	if !uf.Connected("a", "b") {
		t.Error("a and b should be connected")
	}
	if !uf.Connected("d", "e") {
		t.Error("d and e should be connected")
	}
	if uf.Connected("a", "c") {
		t.Error("a and c should not be connected")
	}
	if uf.Connected("b", "d") {
		t.Error("b and d should not be connected")
	}
}

// --- Track partitioning tests ---

func TestComputeTracks_SingleChain(t *testing.T) {
	t.Parallel()
	// A → B → C → D: all in one track.
	d := buildChain(t)

	tracks, err := d.ComputeTracks()
	if err != nil {
		t.Fatalf("ComputeTracks: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}
	if len(tracks[0].NodeIDs) != 4 {
		t.Errorf("track has %d nodes, want 4", len(tracks[0].NodeIDs))
	}
	// Should be in topological order: D before C before B before A.
	assertTopoOrder(t, d, tracks[0].NodeIDs)
}

func TestComputeTracks_FullyDisconnected(t *testing.T) {
	t.Parallel()
	// Four isolated nodes — each is its own track.
	d := New()
	for _, id := range []string{"A", "B", "C", "D"} {
		if err := d.AddNode(id, 0); err != nil {
			t.Fatal(err)
		}
	}

	tracks, err := d.ComputeTracks()
	if err != nil {
		t.Fatalf("ComputeTracks: %v", err)
	}
	if len(tracks) != 4 {
		t.Fatalf("got %d tracks, want 4", len(tracks))
	}
	for _, tr := range tracks {
		if len(tr.NodeIDs) != 1 {
			t.Errorf("track %d has %d nodes, want 1", tr.ID, len(tr.NodeIDs))
		}
	}
}

func TestComputeTracks_TwoIndependentChains(t *testing.T) {
	t.Parallel()
	// Chain1: A → B, Chain2: C → D — two independent tracks.
	d := New()
	for _, id := range []string{"A", "B", "C", "D"} {
		if err := d.AddNode(id, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.AddEdge("A", "B"); err != nil {
		t.Fatal(err)
	}
	if err := d.AddEdge("C", "D"); err != nil {
		t.Fatal(err)
	}

	tracks, err := d.ComputeTracks()
	if err != nil {
		t.Fatalf("ComputeTracks: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2", len(tracks))
	}

	// Each track should have exactly 2 nodes.
	for _, tr := range tracks {
		if len(tr.NodeIDs) != 2 {
			t.Errorf("track %d has %d nodes, want 2", tr.ID, len(tr.NodeIDs))
		}
		assertTopoOrder(t, d, tr.NodeIDs)
	}

	// The two tracks should contain different sets of nodes.
	all := make(map[string]int)
	for _, tr := range tracks {
		for _, id := range tr.NodeIDs {
			all[id] = tr.ID
		}
	}
	if all["A"] == all["C"] {
		t.Error("A and C should be in different tracks")
	}
}

func TestComputeTracks_Diamond(t *testing.T) {
	t.Parallel()
	// A → B → D, A → C → D: all connected — one track.
	d := buildDiamond(t)

	tracks, err := d.ComputeTracks()
	if err != nil {
		t.Fatalf("ComputeTracks: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}
	if len(tracks[0].NodeIDs) != 4 {
		t.Errorf("track has %d nodes, want 4", len(tracks[0].NodeIDs))
	}
	assertTopoOrder(t, d, tracks[0].NodeIDs)
}

func TestComputeTracks_MixedConnectedAndIsolated(t *testing.T) {
	t.Parallel()
	// Chain: A → B → C, Isolated: X, Y — three tracks.
	d := New()
	for _, id := range []string{"A", "B", "C", "X", "Y"} {
		if err := d.AddNode(id, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.AddEdge("A", "B"); err != nil {
		t.Fatal(err)
	}
	if err := d.AddEdge("B", "C"); err != nil {
		t.Fatal(err)
	}

	tracks, err := d.ComputeTracks()
	if err != nil {
		t.Fatalf("ComputeTracks: %v", err)
	}
	if len(tracks) != 3 {
		t.Fatalf("got %d tracks, want 3", len(tracks))
	}

	// Find the track with 3 nodes.
	var chainTrack *Track
	singleCount := 0
	for i := range tracks {
		if len(tracks[i].NodeIDs) == 3 {
			chainTrack = &tracks[i]
		} else if len(tracks[i].NodeIDs) == 1 {
			singleCount++
		}
	}
	if chainTrack == nil {
		t.Fatal("expected one track with 3 nodes")
	}
	if singleCount != 2 {
		t.Errorf("expected 2 single-node tracks, got %d", singleCount)
	}
	assertTopoOrder(t, d, chainTrack.NodeIDs)
}

func TestComputeTracks_TrackIDsAssigned(t *testing.T) {
	t.Parallel()
	// Two chains: A → B, C → D — each node should get its track's ID.
	d := New()
	for _, id := range []string{"A", "B", "C", "D"} {
		if err := d.AddNode(id, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.AddEdge("A", "B"); err != nil {
		t.Fatal(err)
	}
	if err := d.AddEdge("C", "D"); err != nil {
		t.Fatal(err)
	}

	tracks, err := d.ComputeTracks()
	if err != nil {
		t.Fatalf("ComputeTracks: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2", len(tracks))
	}

	for _, tr := range tracks {
		for _, id := range tr.NodeIDs {
			node := d.Node(id)
			if node.TrackID != tr.ID {
				t.Errorf("node %q TrackID = %d, want %d", id, node.TrackID, tr.ID)
			}
		}
	}
}

func TestComputeTracks_AggregateImpact(t *testing.T) {
	t.Parallel()
	d := New()
	for _, id := range []string{"A", "B", "C"} {
		if err := d.AddNode(id, 0); err != nil {
			t.Fatal(err)
		}
	}
	// A and B connected, C isolated.
	if err := d.AddEdge("A", "B"); err != nil {
		t.Fatal(err)
	}

	// Set impact scores manually.
	d.Node("A").Impact = 0.5
	d.Node("B").Impact = 0.3
	d.Node("C").Impact = 0.1

	tracks, err := d.ComputeTracks()
	if err != nil {
		t.Fatalf("ComputeTracks: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2", len(tracks))
	}

	// Tracks are sorted by descending aggregate impact, so the {A,B}
	// track (0.8) should come first, then {C} (0.1).
	abTrack := tracks[0]
	cTrack := tracks[1]

	const epsilon = 1e-9
	if diff := abTrack.AggregateImpact - 0.8; diff > epsilon || diff < -epsilon {
		t.Errorf("AB track AggregateImpact = %f, want 0.8", abTrack.AggregateImpact)
	}
	if diff := abTrack.MaxImpact - 0.5; diff > epsilon || diff < -epsilon {
		t.Errorf("AB track MaxImpact = %f, want 0.5", abTrack.MaxImpact)
	}
	if diff := cTrack.AggregateImpact - 0.1; diff > epsilon || diff < -epsilon {
		t.Errorf("C track AggregateImpact = %f, want 0.1", cTrack.AggregateImpact)
	}
}

func TestComputeTracks_EmptyDAG(t *testing.T) {
	t.Parallel()
	d := New()

	tracks, err := d.ComputeTracks()
	if err != nil {
		t.Fatalf("ComputeTracks: %v", err)
	}
	if tracks != nil {
		t.Errorf("expected nil tracks for empty DAG, got %v", tracks)
	}
}

func TestComputeTracks_SingleNode(t *testing.T) {
	t.Parallel()
	d := New()
	if err := d.AddNode("X", 5); err != nil {
		t.Fatal(err)
	}

	tracks, err := d.ComputeTracks()
	if err != nil {
		t.Fatalf("ComputeTracks: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}
	if tracks[0].NodeIDs[0] != "X" {
		t.Errorf("expected node X in track, got %v", tracks[0].NodeIDs)
	}
	if d.Node("X").TrackID != 0 {
		t.Errorf("Node X TrackID = %d, want 0", d.Node("X").TrackID)
	}
}

func TestComputeTracks_FanIn(t *testing.T) {
	t.Parallel()
	// A, B, C all depend on D — single connected component.
	d := buildFanIn(t)

	tracks, err := d.ComputeTracks()
	if err != nil {
		t.Fatalf("ComputeTracks: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}
	if len(tracks[0].NodeIDs) != 4 {
		t.Errorf("track has %d nodes, want 4", len(tracks[0].NodeIDs))
	}
	assertTopoOrder(t, d, tracks[0].NodeIDs)
}

func TestComputeTracks_TopoOrderWithinTrack(t *testing.T) {
	t.Parallel()
	// A → B → C in one track, verify ordering is C, B, A
	// (dependencies before dependents).
	d := New()
	for _, id := range []string{"A", "B", "C"} {
		if err := d.AddNode(id, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.AddEdge("A", "B"); err != nil {
		t.Fatal(err)
	}
	if err := d.AddEdge("B", "C"); err != nil {
		t.Fatal(err)
	}

	tracks, err := d.ComputeTracks()
	if err != nil {
		t.Fatalf("ComputeTracks: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}
	assertTopoOrder(t, d, tracks[0].NodeIDs)
}

// assertTopoOrder checks that within the given node ID list, every
// dependency of a node appears before it.
func assertTopoOrder(t *testing.T, d *DAG, ids []string) {
	t.Helper()
	pos := make(map[string]int, len(ids))
	for i, id := range ids {
		pos[id] = i
	}
	for _, id := range ids {
		for dep := range d.adjacency[id] {
			depPos, ok := pos[dep]
			if !ok {
				continue // dependency is in another track
			}
			if depPos >= pos[id] {
				// Sort IDs for deterministic error message.
				sorted := make([]string, len(ids))
				copy(sorted, ids)
				sort.Strings(sorted)
				t.Errorf("topological violation: %q (pos %d) depends on %q (pos %d) in order %v",
					id, pos[id], dep, depPos, ids)
			}
		}
	}
}
