package nebula

import (
	"errors"
	"testing"
)

func TestValidateHotAdd(t *testing.T) {
	t.Parallel()

	t.Run("valid phase no deps", func(t *testing.T) {
		t.Parallel()
		d, _ := phasesToDAG([]PhaseSpec{{ID: "a", Title: "A"}})
		errs := ValidateHotAdd(PhaseSpec{ID: "b", Title: "B"}, map[string]bool{"a": true}, d)
		if len(errs) > 0 {
			t.Errorf("expected no errors, got %v", errs)
		}
	})

	t.Run("valid phase with deps", func(t *testing.T) {
		t.Parallel()
		d, _ := phasesToDAG([]PhaseSpec{{ID: "a", Title: "A"}})
		errs := ValidateHotAdd(PhaseSpec{ID: "b", Title: "B", DependsOn: []string{"a"}}, map[string]bool{"a": true}, d)
		if len(errs) > 0 {
			t.Errorf("expected no errors, got %v", errs)
		}
	})

	t.Run("missing id", func(t *testing.T) {
		t.Parallel()
		d, _ := phasesToDAG([]PhaseSpec{{ID: "a", Title: "A"}})
		errs := ValidateHotAdd(PhaseSpec{Title: "No ID"}, map[string]bool{"a": true}, d)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d", len(errs))
		}
		if !errors.Is(errs[0].Err, ErrMissingField) {
			t.Errorf("expected ErrMissingField, got %v", errs[0].Err)
		}
	})

	t.Run("missing title", func(t *testing.T) {
		t.Parallel()
		d, _ := phasesToDAG([]PhaseSpec{{ID: "a", Title: "A"}})
		errs := ValidateHotAdd(PhaseSpec{ID: "b"}, map[string]bool{"a": true}, d)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d", len(errs))
		}
		if !errors.Is(errs[0].Err, ErrMissingField) {
			t.Errorf("expected ErrMissingField, got %v", errs[0].Err)
		}
	})

	t.Run("duplicate id", func(t *testing.T) {
		t.Parallel()
		d, _ := phasesToDAG([]PhaseSpec{{ID: "a", Title: "A"}})
		errs := ValidateHotAdd(PhaseSpec{ID: "a", Title: "Dup"}, map[string]bool{"a": true}, d)
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
		d, _ := phasesToDAG([]PhaseSpec{
			{ID: "a", Title: "A"},
			{ID: "b", Title: "B", DependsOn: []string{"a"}},
		})
		errs := ValidateHotAdd(
			PhaseSpec{ID: "c", Title: "C", DependsOn: []string{"b"}, Blocks: []string{"a"}},
			map[string]bool{"a": true, "b": true},
			d,
		)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d", len(errs))
		}
		if !errors.Is(errs[0].Err, ErrDependencyCycle) {
			t.Errorf("expected ErrDependencyCycle, got %v", errs[0].Err)
		}

		// DAG should be rolled back: c should not exist.
		if d.Node("c") != nil {
			t.Error("expected graph rollback: c should not exist in DAG")
		}
	})

	t.Run("blocks field adds edges", func(t *testing.T) {
		t.Parallel()
		d, _ := phasesToDAG([]PhaseSpec{
			{ID: "a", Title: "A"},
			{ID: "b", Title: "B", DependsOn: []string{"a"}},
		})
		errs := ValidateHotAdd(
			PhaseSpec{ID: "c", Title: "C", DependsOn: []string{"a"}, Blocks: []string{"b"}},
			map[string]bool{"a": true, "b": true},
			d,
		)
		if len(errs) > 0 {
			t.Errorf("expected no errors, got %v", errs)
		}
		// b should now depend on c (reverse edge from blocks).
		if !d.HasPath("b", "c") {
			t.Error("expected b to depend on c after blocks injection")
		}
	})
}

func TestRollbackHotAdd(t *testing.T) {
	t.Parallel()
	d, _ := phasesToDAG([]PhaseSpec{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
	})

	phase := PhaseSpec{ID: "c", Title: "C", DependsOn: []string{"a"}, Blocks: []string{"b"}}
	d.AddNodeIdempotent("c", 0)
	_ = d.AddEdge("c", "a")
	_ = d.AddEdge("b", "c")

	rollbackHotAdd(d, phase)

	if d.Node("c") != nil {
		t.Error("expected c to be removed from DAG after rollback")
	}
	if d.HasPath("b", "c") {
		t.Error("expected b→c edge to be removed after rollback")
	}
}
