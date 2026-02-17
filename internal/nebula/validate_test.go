package nebula

import (
	"errors"
	"testing"
)

func TestValidateHotAdd(t *testing.T) {
	t.Parallel()

	t.Run("valid phase no deps", func(t *testing.T) {
		t.Parallel()
		graph := NewGraph([]PhaseSpec{{ID: "a", Title: "A"}})
		errs := ValidateHotAdd(PhaseSpec{ID: "b", Title: "B"}, map[string]bool{"a": true}, graph)
		if len(errs) > 0 {
			t.Errorf("expected no errors, got %v", errs)
		}
	})

	t.Run("valid phase with deps", func(t *testing.T) {
		t.Parallel()
		graph := NewGraph([]PhaseSpec{{ID: "a", Title: "A"}})
		errs := ValidateHotAdd(PhaseSpec{ID: "b", Title: "B", DependsOn: []string{"a"}}, map[string]bool{"a": true}, graph)
		if len(errs) > 0 {
			t.Errorf("expected no errors, got %v", errs)
		}
	})

	t.Run("missing id", func(t *testing.T) {
		t.Parallel()
		graph := NewGraph([]PhaseSpec{{ID: "a", Title: "A"}})
		errs := ValidateHotAdd(PhaseSpec{Title: "No ID"}, map[string]bool{"a": true}, graph)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d", len(errs))
		}
		if !errors.Is(errs[0].Err, ErrMissingField) {
			t.Errorf("expected ErrMissingField, got %v", errs[0].Err)
		}
	})

	t.Run("missing title", func(t *testing.T) {
		t.Parallel()
		graph := NewGraph([]PhaseSpec{{ID: "a", Title: "A"}})
		errs := ValidateHotAdd(PhaseSpec{ID: "b"}, map[string]bool{"a": true}, graph)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d", len(errs))
		}
		if !errors.Is(errs[0].Err, ErrMissingField) {
			t.Errorf("expected ErrMissingField, got %v", errs[0].Err)
		}
	})

	t.Run("duplicate id", func(t *testing.T) {
		t.Parallel()
		graph := NewGraph([]PhaseSpec{{ID: "a", Title: "A"}})
		errs := ValidateHotAdd(PhaseSpec{ID: "a", Title: "Dup"}, map[string]bool{"a": true}, graph)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d", len(errs))
		}
		if !errors.Is(errs[0].Err, ErrDuplicateID) {
			t.Errorf("expected ErrDuplicateID, got %v", errs[0].Err)
		}
	})

	t.Run("cycle detection", func(t *testing.T) {
		t.Parallel()
		// a → b (b depends on a). Adding c that depends on b and blocks a creates cycle.
		graph := NewGraph([]PhaseSpec{
			{ID: "a", Title: "A"},
			{ID: "b", Title: "B", DependsOn: []string{"a"}},
		})
		errs := ValidateHotAdd(
			PhaseSpec{ID: "c", Title: "C", DependsOn: []string{"b"}, Blocks: []string{"a"}},
			map[string]bool{"a": true, "b": true},
			graph,
		)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d", len(errs))
		}
		if !errors.Is(errs[0].Err, ErrDependencyCycle) {
			t.Errorf("expected ErrDependencyCycle, got %v", errs[0].Err)
		}

		// Graph should be rolled back: c should not exist.
		_, hasC := graph.adjacency["c"]
		if hasC && len(graph.adjacency["c"]) > 0 {
			t.Error("expected graph rollback: c should not have edges")
		}
	})

	t.Run("blocks field adds edges", func(t *testing.T) {
		t.Parallel()
		graph := NewGraph([]PhaseSpec{
			{ID: "a", Title: "A"},
			{ID: "b", Title: "B", DependsOn: []string{"a"}},
		})
		errs := ValidateHotAdd(
			PhaseSpec{ID: "c", Title: "C", DependsOn: []string{"a"}, Blocks: []string{"b"}},
			map[string]bool{"a": true, "b": true},
			graph,
		)
		if len(errs) > 0 {
			t.Errorf("expected no errors, got %v", errs)
		}
		// b should now depend on c (reverse edge from blocks).
		if !graph.adjacency["b"]["c"] {
			t.Error("expected b to depend on c after blocks injection")
		}
	})
}

func TestRollbackHotAdd(t *testing.T) {
	t.Parallel()
	graph := NewGraph([]PhaseSpec{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
	})

	phase := PhaseSpec{ID: "c", Title: "C", DependsOn: []string{"a"}, Blocks: []string{"b"}}
	graph.AddNode("c")
	graph.AddEdge("c", "a")
	graph.AddEdge("b", "c")

	rollbackHotAdd(graph, phase)

	if _, hasC := graph.adjacency["c"]; hasC {
		t.Error("expected c to be removed from adjacency after rollback")
	}
	if graph.adjacency["b"]["c"] {
		t.Error("expected b→c edge to be removed after rollback")
	}
}

func TestCheckHotAddedReady(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	neb := &Nebula{
		Dir:      dir,
		Manifest: Manifest{},
		Phases: []PhaseSpec{
			{ID: "a", Title: "A"},
			{ID: "b", Title: "B", DependsOn: []string{"a"}},
		},
	}
	state := &State{
		Version: 1,
		Phases: map[string]*PhaseState{
			"a": {Status: PhaseStatusDone},
			"b": {Status: PhaseStatusPending},
		},
	}
	graph := NewGraph(neb.Phases)
	hotAdded := make(chan string, 16)
	wg := &WorkerGroup{
		Nebula:         neb,
		State:          state,
		liveGraph:      graph,
		livePhasesByID: map[string]*PhaseSpec{"a": &neb.Phases[0], "b": &neb.Phases[1]},
		liveDone:       map[string]bool{"a": true},
		liveFailed:     map[string]bool{},
		liveInFlight:   map[string]bool{},
		hotAdded:       hotAdded,
	}

	// b has deps satisfied and is pending — should be signaled.
	wg.checkHotAddedReady()

	select {
	case id := <-hotAdded:
		if id != "b" {
			t.Errorf("expected b on hotAdded, got %q", id)
		}
	default:
		t.Error("expected b to be signaled as ready")
	}
}
