package nebula

import "testing"

func TestWouldCollide(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		candidate *PhaseSpec
		others    []*PhaseSpec
		wantCount int
		wantOther string // OtherPhaseID of the first collision, if any
	}{
		{
			name:      "overlapping scope yields one collision",
			candidate: &PhaseSpec{ID: "b", Scope: []string{"internal/runtime/**"}},
			others:    []*PhaseSpec{{ID: "a", Scope: []string{"internal/runtime/engine.go"}}},
			wantCount: 1,
			wantOther: "a",
		},
		{
			name:      "disjoint scope yields no collision",
			candidate: &PhaseSpec{ID: "b", Scope: []string{"internal/ui/**"}},
			others:    []*PhaseSpec{{ID: "a", Scope: []string{"internal/runtime/**"}}},
			wantCount: 0,
		},
		{
			name:      "candidate AllowScopeOverlap suppresses collision",
			candidate: &PhaseSpec{ID: "b", Scope: []string{"internal/runtime/**"}, AllowScopeOverlap: true},
			others:    []*PhaseSpec{{ID: "a", Scope: []string{"internal/runtime/**"}}},
			wantCount: 0,
		},
		{
			name:      "other AllowScopeOverlap suppresses collision",
			candidate: &PhaseSpec{ID: "b", Scope: []string{"internal/runtime/**"}},
			others:    []*PhaseSpec{{ID: "a", Scope: []string{"internal/runtime/**"}, AllowScopeOverlap: true}},
			wantCount: 0,
		},
		{
			name:      "candidate empty scope yields no collision",
			candidate: &PhaseSpec{ID: "b"},
			others:    []*PhaseSpec{{ID: "a", Scope: []string{"internal/runtime/**"}}},
			wantCount: 0,
		},
		{
			name:      "self is skipped",
			candidate: &PhaseSpec{ID: "a", Scope: []string{"internal/runtime/**"}},
			others:    []*PhaseSpec{{ID: "a", Scope: []string{"internal/runtime/**"}}},
			wantCount: 0,
		},
		{
			name:      "nil and empty-scope others are skipped",
			candidate: &PhaseSpec{ID: "b", Scope: []string{"internal/runtime/**"}},
			others:    []*PhaseSpec{nil, {ID: "c"}, {ID: "a", Scope: []string{"internal/runtime/engine.go"}}},
			wantCount: 1,
			wantOther: "a",
		},
		{
			name:      "multiple overlapping others yield multiple collisions",
			candidate: &PhaseSpec{ID: "c", Scope: []string{"internal/runtime/**"}},
			others: []*PhaseSpec{
				{ID: "a", Scope: []string{"internal/runtime/engine.go"}},
				{ID: "b", Scope: []string{"internal/runtime/budget.go"}},
			},
			wantCount: 2,
			wantOther: "a",
		},
		{
			name:      "nil candidate yields no collision",
			candidate: nil,
			others:    []*PhaseSpec{{ID: "a", Scope: []string{"internal/runtime/**"}}},
			wantCount: 0,
		},
	}

	det := NewCollisionDetector(nil)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := det.WouldCollide(tc.candidate, tc.others)
			if len(got) != tc.wantCount {
				t.Fatalf("WouldCollide returned %d collisions, want %d: %+v", len(got), tc.wantCount, got)
			}
			if tc.wantCount > 0 {
				if got[0].OtherPhaseID != tc.wantOther {
					t.Errorf("first collision OtherPhaseID = %q, want %q", got[0].OtherPhaseID, tc.wantOther)
				}
				if got[0].Kind != CollisionKindScope {
					t.Errorf("collision Kind = %q, want %q", got[0].Kind, CollisionKindScope)
				}
				if got[0].Action != CollisionActionWait {
					t.Errorf("collision Action = %q, want %q", got[0].Action, CollisionActionWait)
				}
				if got[0].Reason == "" {
					t.Error("collision Reason is empty")
				}
			}
		})
	}
}

func TestPhaseTrackerCollisions(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "a", Scope: []string{"internal/runtime/**"}},
		{ID: "b", Scope: []string{"internal/runtime/engine.go"}},
		{ID: "c", Scope: []string{"internal/ui/**"}},
	}
	state := &State{Phases: map[string]*PhaseState{}}
	pt := NewPhaseTracker(phases, state)
	pt.inFlight["a"] = true

	// b overlaps in-flight a; c does not.
	got := pt.Collisions([]string{"b", "c"})
	if len(got) != 1 {
		t.Fatalf("Collisions returned %d, want 1: %+v", len(got), got)
	}
	if got[0].OtherPhaseID != "a" {
		t.Errorf("collision OtherPhaseID = %q, want %q", got[0].OtherPhaseID, "a")
	}
}
