package nebula

import "testing"

func TestNebulaSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("independence from original", func(t *testing.T) {
		t.Parallel()
		n := &Nebula{
			Dir: "/tmp/test",
			Phases: []PhaseSpec{
				{ID: "a", Title: "Phase A", DependsOn: []string{"x"}},
				{ID: "b", Title: "Phase B"},
			},
		}

		snap := n.Snapshot()

		// Append to original should not affect snapshot.
		n.Phases = append(n.Phases, PhaseSpec{ID: "c", Title: "Phase C"})
		if len(snap.Phases) != 2 {
			t.Fatalf("snapshot Phases length = %d, want 2", len(snap.Phases))
		}
	})

	t.Run("deep copy of sub-slices", func(t *testing.T) {
		t.Parallel()
		n := &Nebula{
			Phases: []PhaseSpec{
				{
					ID:        "a",
					DependsOn: []string{"x", "y"},
					Labels:    []string{"l1"},
					Scope:     []string{"s1", "s2"},
					Blocks:    []string{"b1"},
				},
			},
		}

		snap := n.Snapshot()

		// Mutate the original's sub-slices.
		n.Phases[0].DependsOn[0] = "CHANGED"
		n.Phases[0].Labels[0] = "CHANGED"
		n.Phases[0].Scope[0] = "CHANGED"
		n.Phases[0].Blocks[0] = "CHANGED"

		if snap.Phases[0].DependsOn[0] != "x" {
			t.Errorf("DependsOn[0] = %q, want %q", snap.Phases[0].DependsOn[0], "x")
		}
		if snap.Phases[0].Labels[0] != "l1" {
			t.Errorf("Labels[0] = %q, want %q", snap.Phases[0].Labels[0], "l1")
		}
		if snap.Phases[0].Scope[0] != "s1" {
			t.Errorf("Scope[0] = %q, want %q", snap.Phases[0].Scope[0], "s1")
		}
		if snap.Phases[0].Blocks[0] != "b1" {
			t.Errorf("Blocks[0] = %q, want %q", snap.Phases[0].Blocks[0], "b1")
		}
	})

	t.Run("nil Phases", func(t *testing.T) {
		t.Parallel()
		n := &Nebula{
			Dir: "/tmp/nil-test",
		}

		snap := n.Snapshot()
		if snap.Phases != nil {
			t.Fatalf("snapshot Phases = %v, want nil", snap.Phases)
		}
		if snap.Dir != "/tmp/nil-test" {
			t.Errorf("Dir = %q, want %q", snap.Dir, "/tmp/nil-test")
		}
	})

	t.Run("scalar fields preserved", func(t *testing.T) {
		t.Parallel()
		n := &Nebula{
			Dir: "/tmp/scalar",
			Manifest: Manifest{
				Nebula: Info{Name: "test-nebula", Description: "desc"},
			},
			Phases: []PhaseSpec{
				{ID: "a", Title: "A", Priority: 3, Assignee: "user1"},
			},
		}

		snap := n.Snapshot()

		if snap.Dir != n.Dir {
			t.Errorf("Dir = %q, want %q", snap.Dir, n.Dir)
		}
		if snap.Manifest.Nebula.Name != "test-nebula" {
			t.Errorf("Manifest.Nebula.Name = %q, want %q", snap.Manifest.Nebula.Name, "test-nebula")
		}
		if snap.Phases[0].Priority != 3 {
			t.Errorf("Phases[0].Priority = %d, want 3", snap.Phases[0].Priority)
		}
		if snap.Phases[0].Assignee != "user1" {
			t.Errorf("Phases[0].Assignee = %q, want %q", snap.Phases[0].Assignee, "user1")
		}
	})
}
