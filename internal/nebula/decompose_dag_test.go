package nebula

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/dag"
)

// buildDAG creates a DAG from a list of nodes and edges for testing.
// Each node is specified as "id:priority". Each edge is "from->to".
func buildDAG(t *testing.T, nodes []string, edges []string) *dag.DAG {
	t.Helper()
	d := dag.New()
	for _, n := range nodes {
		parts := strings.SplitN(n, ":", 2)
		id := parts[0]
		priority := 0
		if len(parts) == 2 {
			fmt.Sscanf(parts[1], "%d", &priority)
		}
		if err := d.AddNode(id, priority); err != nil {
			t.Fatalf("AddNode(%s): %v", id, err)
		}
	}
	for _, e := range edges {
		parts := strings.SplitN(e, "->", 2)
		if err := d.AddEdge(parts[0], parts[1]); err != nil {
			t.Fatalf("AddEdge(%s, %s): %v", parts[0], parts[1], err)
		}
	}
	return d
}

func TestApplyDecomposition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		nodes       []string   // "id:priority"
		edges       []string   // "from->to"
		op          DecomposeOp
		wantIDs     []string
		wantErr     string
		checkGraph  func(t *testing.T, d *dag.DAG)
	}{
		{
			name:  "simple two sub-phases",
			nodes: []string{"A:1", "P:2", "X:3"},
			edges: []string{"P->A", "X->P"},
			op: DecomposeOp{
				OriginalPhaseID: "P",
				SubPhases: []SubPhaseEntry{
					{Spec: PhaseSpec{ID: "S1", Priority: 2}},
					{Spec: PhaseSpec{ID: "S2", Priority: 2}},
				},
			},
			wantIDs: []string{"S1", "S2"},
			checkGraph: func(t *testing.T, d *dag.DAG) {
				t.Helper()
				// P should be removed.
				if d.Node("P") != nil {
					t.Error("original phase P should be removed")
				}
				// S1 and S2 should exist.
				if d.Node("S1") == nil {
					t.Error("sub-phase S1 should exist")
				}
				if d.Node("S2") == nil {
					t.Error("sub-phase S2 should exist")
				}
				// S1 and S2 should depend on A (P's predecessor).
				assertDepsContain(t, d, "S1", "A")
				assertDepsContain(t, d, "S2", "A")
				// X should depend on both S1 and S2.
				assertDepsContain(t, d, "X", "S1")
				assertDepsContain(t, d, "X", "S2")
				// Total nodes: A, S1, S2, X = 4.
				if d.Len() != 4 {
					t.Errorf("expected 4 nodes, got %d", d.Len())
				}
			},
		},
		{
			name:  "three sub-phases with inter-dependencies",
			nodes: []string{"A:1", "B:1", "P:2", "X:3", "Y:3"},
			edges: []string{"P->A", "P->B", "X->P", "Y->P"},
			op: DecomposeOp{
				OriginalPhaseID: "P",
				SubPhases: []SubPhaseEntry{
					{Spec: PhaseSpec{ID: "S1", Priority: 2}},
					{Spec: PhaseSpec{ID: "S2", Priority: 2, DependsOn: []string{"S1"}}},
					{Spec: PhaseSpec{ID: "S3", Priority: 2, DependsOn: []string{"S1"}}},
				},
			},
			wantIDs: []string{"S1", "S2", "S3"},
			checkGraph: func(t *testing.T, d *dag.DAG) {
				t.Helper()
				if d.Node("P") != nil {
					t.Error("original phase P should be removed")
				}
				// All sub-phases should depend on A and B.
				for _, sub := range []string{"S1", "S2", "S3"} {
					assertDepsContain(t, d, sub, "A")
					assertDepsContain(t, d, sub, "B")
				}
				// S2 and S3 should additionally depend on S1.
				assertDepsContain(t, d, "S2", "S1")
				assertDepsContain(t, d, "S3", "S1")
				// X and Y should depend on all sub-phases.
				for _, succ := range []string{"X", "Y"} {
					assertDepsContain(t, d, succ, "S1")
					assertDepsContain(t, d, succ, "S2")
					assertDepsContain(t, d, succ, "S3")
				}
			},
		},
		{
			name:  "empty predecessors",
			nodes: []string{"P:2", "X:3"},
			edges: []string{"X->P"},
			op: DecomposeOp{
				OriginalPhaseID: "P",
				SubPhases: []SubPhaseEntry{
					{Spec: PhaseSpec{ID: "S1", Priority: 2}},
					{Spec: PhaseSpec{ID: "S2", Priority: 2}},
				},
			},
			wantIDs: []string{"S1", "S2"},
			checkGraph: func(t *testing.T, d *dag.DAG) {
				t.Helper()
				// S1 and S2 should have no predecessors (P had none).
				if deps := d.DepsFor("S1"); len(deps) != 0 {
					t.Errorf("S1 should have no deps, got %v", deps)
				}
				if deps := d.DepsFor("S2"); len(deps) != 0 {
					t.Errorf("S2 should have no deps, got %v", deps)
				}
				// X should depend on S1 and S2.
				assertDepsContain(t, d, "X", "S1")
				assertDepsContain(t, d, "X", "S2")
			},
		},
		{
			name:  "empty successors",
			nodes: []string{"A:1", "P:2"},
			edges: []string{"P->A"},
			op: DecomposeOp{
				OriginalPhaseID: "P",
				SubPhases: []SubPhaseEntry{
					{Spec: PhaseSpec{ID: "S1", Priority: 2}},
					{Spec: PhaseSpec{ID: "S2", Priority: 2}},
				},
			},
			wantIDs: []string{"S1", "S2"},
			checkGraph: func(t *testing.T, d *dag.DAG) {
				t.Helper()
				// S1 and S2 should depend on A.
				assertDepsContain(t, d, "S1", "A")
				assertDepsContain(t, d, "S2", "A")
				// No node should depend on S1 or S2 (P had no successors).
				for _, nodeID := range d.Nodes() {
					if nodeID == "S1" || nodeID == "S2" || nodeID == "A" {
						continue
					}
					t.Errorf("unexpected node %s", nodeID)
				}
			},
		},
		{
			name:  "duplicate sub-phase ID",
			nodes: []string{"P:2"},
			edges: nil,
			op: DecomposeOp{
				OriginalPhaseID: "P",
				SubPhases: []SubPhaseEntry{
					{Spec: PhaseSpec{ID: "S1", Priority: 2}},
					{Spec: PhaseSpec{ID: "S1", Priority: 2}},
				},
			},
			wantErr: "duplicate sub-phase ID",
		},
		{
			name:  "original phase not found",
			nodes: []string{"A:1"},
			edges: nil,
			op: DecomposeOp{
				OriginalPhaseID: "nonexistent",
				SubPhases: []SubPhaseEntry{
					{Spec: PhaseSpec{ID: "S1", Priority: 2}},
				},
			},
			wantErr: "node not found",
		},
		{
			name:  "no sub-phases",
			nodes: []string{"P:2"},
			edges: nil,
			op: DecomposeOp{
				OriginalPhaseID: "P",
				SubPhases:       nil,
			},
			wantErr: "no sub-phases provided",
		},
		{
			name:  "sub-phase ID conflicts with existing node",
			nodes: []string{"P:2", "X:3"},
			edges: nil,
			op: DecomposeOp{
				OriginalPhaseID: "P",
				SubPhases: []SubPhaseEntry{
					{Spec: PhaseSpec{ID: "X", Priority: 2}},
				},
			},
			wantErr: "already exists in DAG",
		},
		{
			name:  "cycle detection via inter-sub-phase deps",
			nodes: []string{"P:2"},
			edges: nil,
			op: DecomposeOp{
				OriginalPhaseID: "P",
				SubPhases: []SubPhaseEntry{
					{Spec: PhaseSpec{ID: "S1", Priority: 2, DependsOn: []string{"S2"}}},
					{Spec: PhaseSpec{ID: "S2", Priority: 2, DependsOn: []string{"S1"}}},
				},
			},
			wantErr: "cycle",
		},
		{
			name:  "preserves priority from sub-phase specs",
			nodes: []string{"P:5"},
			edges: nil,
			op: DecomposeOp{
				OriginalPhaseID: "P",
				SubPhases: []SubPhaseEntry{
					{Spec: PhaseSpec{ID: "S1", Priority: 3}},
					{Spec: PhaseSpec{ID: "S2", Priority: 7}},
				},
			},
			wantIDs: []string{"S1", "S2"},
			checkGraph: func(t *testing.T, d *dag.DAG) {
				t.Helper()
				if p := d.Node("S1").Priority; p != 3 {
					t.Errorf("S1 priority: got %d, want 3", p)
				}
				if p := d.Node("S2").Priority; p != 7 {
					t.Errorf("S2 priority: got %d, want 7", p)
				}
			},
		},
		{
			name:  "diamond dependency pattern",
			nodes: []string{"A:1", "P:2", "X:3"},
			edges: []string{"P->A", "X->P"},
			op: DecomposeOp{
				OriginalPhaseID: "P",
				SubPhases: []SubPhaseEntry{
					{Spec: PhaseSpec{ID: "S1", Priority: 2}},
					{Spec: PhaseSpec{ID: "S2", Priority: 2, DependsOn: []string{"S1"}}},
				},
			},
			wantIDs: []string{"S1", "S2"},
			checkGraph: func(t *testing.T, d *dag.DAG) {
				t.Helper()
				// Verify the DAG is valid (no cycles).
				if _, err := d.TopologicalSort(); err != nil {
					t.Errorf("DAG has cycle after decomposition: %v", err)
				}
				// A -> S1 -> S2 -> X, plus A -> S2, S1 -> X
				assertDepsContain(t, d, "S1", "A")
				assertDepsContain(t, d, "S2", "A")
				assertDepsContain(t, d, "S2", "S1")
				assertDepsContain(t, d, "X", "S1")
				assertDepsContain(t, d, "X", "S2")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := buildDAG(t, tt.nodes, tt.edges)

			gotIDs, err := ApplyDecomposition(d, tt.op)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(gotIDs) != len(tt.wantIDs) {
				t.Errorf("got %d sub-phase IDs, want %d", len(gotIDs), len(tt.wantIDs))
			}

			if tt.checkGraph != nil {
				tt.checkGraph(t, d)
			}
		})
	}
}

func TestApplyDecompositionToNebula(t *testing.T) {
	t.Parallel()

	t.Run("writes phase files and updates registry", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		// Write original phase file.
		origContent := "+++\nid = \"P\"\ntitle = \"Original\"\npriority = 2\n+++\n\nOriginal body.\n"
		if err := os.WriteFile(filepath.Join(dir, "02-original.md"), []byte(origContent), 0o644); err != nil {
			t.Fatal(err)
		}

		neb := &Nebula{
			Dir: dir,
			Phases: []PhaseSpec{
				{ID: "A", Priority: 1, SourceFile: "01-a.md"},
				{ID: "P", Priority: 2, SourceFile: "02-original.md"},
				{ID: "X", Priority: 3, SourceFile: "03-x.md"},
			},
		}
		phasesByID := PhasesByID(neb.Phases)

		d := dag.New()
		d.AddNodeIdempotent("A", 1)
		d.AddNodeIdempotent("P", 2)
		d.AddNodeIdempotent("X", 3)
		if err := d.AddEdge("P", "A"); err != nil {
			t.Fatal(err)
		}
		if err := d.AddEdge("X", "P"); err != nil {
			t.Fatal(err)
		}

		op := DecomposeOp{
			OriginalPhaseID: "P",
			SubPhases: []SubPhaseEntry{
				{
					Spec:     PhaseSpec{ID: "S1", Title: "Sub 1", Priority: 2},
					Body:     "Sub-phase 1 body.",
					Filename: "02a-s1.md",
				},
				{
					Spec:     PhaseSpec{ID: "S2", Title: "Sub 2", Priority: 2},
					Body:     "Sub-phase 2 body.",
					Filename: "02b-s2.md",
				},
			},
		}

		subIDs, err := ApplyDecompositionToNebula(neb, d, op, phasesByID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify returned sub-phase IDs.
		if len(subIDs) != 2 {
			t.Fatalf("expected 2 sub-phase IDs, got %d", len(subIDs))
		}

		// Verify phase files were written.
		for _, filename := range []string{"02a-s1.md", "02b-s2.md"} {
			path := filepath.Join(dir, filename)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("expected file %s to exist: %v", filename, err)
				continue
			}
			if !strings.HasPrefix(string(content), "+++\n") {
				t.Errorf("file %s should start with +++ frontmatter", filename)
			}
		}

		// Verify phase registry was updated.
		if _, ok := phasesByID["P"]; ok {
			t.Error("original phase P should be removed from phasesByID")
		}
		if _, ok := phasesByID["S1"]; !ok {
			t.Error("sub-phase S1 should be in phasesByID")
		}
		if _, ok := phasesByID["S2"]; !ok {
			t.Error("sub-phase S2 should be in phasesByID")
		}

		// Verify the original phase file was annotated.
		origData, err := os.ReadFile(filepath.Join(dir, "02-original.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(origData), "decomposed = true") {
			t.Error("original phase file should contain decomposed = true annotation")
		}

		// Verify DAG state.
		if d.Node("P") != nil {
			t.Error("P should be removed from DAG")
		}
		assertDepsContain(t, d, "X", "S1")
		assertDepsContain(t, d, "X", "S2")
	})

	t.Run("sub-phase body preserved in written file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		neb := &Nebula{
			Dir: dir,
			Phases: []PhaseSpec{
				{ID: "P", Priority: 2},
			},
		}
		phasesByID := PhasesByID(neb.Phases)

		d := dag.New()
		d.AddNodeIdempotent("P", 2)

		op := DecomposeOp{
			OriginalPhaseID: "P",
			SubPhases: []SubPhaseEntry{
				{
					Spec:     PhaseSpec{ID: "S1", Title: "Sub 1", Priority: 2},
					Body:     "## Problem\n\nDetailed problem description.",
					Filename: "s1.md",
				},
			},
		}

		_, err := ApplyDecompositionToNebula(neb, d, op, phasesByID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		content, err := os.ReadFile(filepath.Join(dir, "s1.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "Detailed problem description") {
			t.Error("phase file should contain the sub-phase body")
		}
	})
}

func TestApplyDecomposition_CycleFromConflictingEdges(t *testing.T) {
	t.Parallel()

	// Build: A -> P -> X, where X also depends on A.
	// Decompose P into S1 (depends on S2) and S2.
	// If S2->S1 would create A->S1->S2->X, then adding S1 depends on S2
	// creates S1->S2 and S2 is also wired via predecessors, creating S2->A.
	// Actually we need to test a genuine cycle from inter-deps.
	d := dag.New()
	d.AddNodeIdempotent("P", 2)

	op := DecomposeOp{
		OriginalPhaseID: "P",
		SubPhases: []SubPhaseEntry{
			{Spec: PhaseSpec{ID: "S1", Priority: 2, DependsOn: []string{"S2"}}},
			{Spec: PhaseSpec{ID: "S2", Priority: 2, DependsOn: []string{"S1"}}},
		},
	}

	_, err := ApplyDecomposition(d, op)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !errors.Is(err, dag.ErrCycle) && !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected cycle-related error, got: %v", err)
	}
}

func TestDirectDependents(t *testing.T) {
	t.Parallel()

	d := dag.New()
	d.AddNodeIdempotent("A", 1)
	d.AddNodeIdempotent("B", 2)
	d.AddNodeIdempotent("C", 3)
	if err := d.AddEdge("B", "A"); err != nil {
		t.Fatal(err)
	}
	if err := d.AddEdge("C", "A"); err != nil {
		t.Fatal(err)
	}

	deps := directDependents(d, "A")
	if len(deps) != 2 {
		t.Fatalf("expected 2 dependents, got %d: %v", len(deps), deps)
	}
	if deps[0] != "B" || deps[1] != "C" {
		t.Errorf("expected [B, C], got %v", deps)
	}

	// Node with no dependents.
	deps = directDependents(d, "C")
	if len(deps) != 0 {
		t.Errorf("expected 0 dependents for C, got %d", len(deps))
	}
}

// assertDepsContain checks that the given node's DepsFor contains the expected dependency.
func assertDepsContain(t *testing.T, d *dag.DAG, nodeID, depID string) {
	t.Helper()
	deps := d.DepsFor(nodeID)
	for _, dep := range deps {
		if dep == depID {
			return
		}
	}
	t.Errorf("%s deps = %v, want to contain %s", nodeID, deps, depID)
}
