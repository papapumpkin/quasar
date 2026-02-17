package nebula

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateScopeOverlaps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		phases    []PhaseSpec
		wantCount int
		wantErr   error
		wantMsg   string // substring in error message
	}{
		{
			name: "overlapping scopes no dependency",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/"}},
				{ID: "b", Title: "B", Scope: []string{"internal/api/"}},
			},
			wantCount: 1,
			wantErr:   ErrScopeOverlap,
			wantMsg:   `phases "a" and "b"`,
		},
		{
			name: "overlapping scopes with dependency",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/"}, DependsOn: []string{"b"}},
				{ID: "b", Title: "B", Scope: []string{"internal/api/"}},
			},
			wantCount: 0,
		},
		{
			name: "overlapping scopes with allow_scope_overlap on first",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/"}, AllowScopeOverlap: true},
				{ID: "b", Title: "B", Scope: []string{"internal/api/"}},
			},
			wantCount: 0,
		},
		{
			name: "overlapping scopes with allow_scope_overlap on second",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/"}},
				{ID: "b", Title: "B", Scope: []string{"internal/api/"}, AllowScopeOverlap: true},
			},
			wantCount: 0,
		},
		{
			name: "unscoped phases skip checking",
			phases: []PhaseSpec{
				{ID: "a", Title: "A"},
				{ID: "b", Title: "B"},
			},
			wantCount: 0,
		},
		{
			name: "one scoped one unscoped",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/"}},
				{ID: "b", Title: "B"},
			},
			wantCount: 0,
		},
		{
			name: "non-overlapping scopes",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/api/"}},
				{ID: "b", Title: "B", Scope: []string{"cmd/"}},
			},
			wantCount: 0,
		},
		{
			name: "exact match scopes",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/api/handler.go"}},
				{ID: "b", Title: "B", Scope: []string{"internal/api/handler.go"}},
			},
			wantCount: 1,
			wantErr:   ErrScopeOverlap,
		},
		{
			name: "directory containment parent contains child",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/"}},
				{ID: "b", Title: "B", Scope: []string{"internal/api/middleware/"}},
			},
			wantCount: 1,
			wantErr:   ErrScopeOverlap,
		},
		{
			name: "error message includes phase IDs and pattern",
			phases: []PhaseSpec{
				{ID: "alpha", Title: "A", Scope: []string{"internal/"}},
				{ID: "beta", Title: "B", Scope: []string{"internal/api/"}},
			},
			wantCount: 1,
			wantErr:   ErrScopeOverlap,
			wantMsg:   `phases "alpha" and "beta"`,
		},
		{
			name: "three phases two overlapping",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/"}},
				{ID: "b", Title: "B", Scope: []string{"cmd/"}},
				{ID: "c", Title: "C", Scope: []string{"internal/loop/"}},
			},
			wantCount: 1,
			wantErr:   ErrScopeOverlap,
		},
		{
			name: "transitive dependency prevents overlap error",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/"}, DependsOn: []string{"b"}},
				{ID: "b", Title: "B", DependsOn: []string{"c"}},
				{ID: "c", Title: "C", Scope: []string{"internal/api/"}},
			},
			wantCount: 0,
		},
		{
			name: "glob star pattern overlap",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/*.go"}},
				{ID: "b", Title: "B", Scope: []string{"internal/api.go"}},
			},
			wantCount: 1,
			wantErr:   ErrScopeOverlap,
		},
		{
			name: "glob doublestar pattern overlap",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/**/*.go"}},
				{ID: "b", Title: "B", Scope: []string{"internal/api/"}},
			},
			wantCount: 1,
			wantErr:   ErrScopeOverlap,
		},
		{
			name: "three phases A-B overlap B-C overlap A-C no overlap",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/api/"}},
				{ID: "b", Title: "B", Scope: []string{"internal/"}},
				{ID: "c", Title: "C", Scope: []string{"internal/loop/"}},
			},
			wantCount: 2,
			wantErr:   ErrScopeOverlap,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			errs := validateScopeOverlaps(tt.phases)
			if len(errs) != tt.wantCount {
				t.Fatalf("got %d errors, want %d: %v", len(errs), tt.wantCount, errs)
			}
			if tt.wantErr != nil && len(errs) > 0 {
				if !errors.Is(errs[0].Err, tt.wantErr) {
					t.Errorf("got error %v, want %v", errs[0].Err, tt.wantErr)
				}
			}
			if tt.wantMsg != "" && len(errs) > 0 {
				if !strings.Contains(errs[0].Err.Error(), tt.wantMsg) {
					t.Errorf("error %q does not contain %q", errs[0].Err.Error(), tt.wantMsg)
				}
			}
		})
	}
}

func TestScopesOverlap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    []string
		b    []string
		want bool
	}{
		{"exact match", []string{"foo.go"}, []string{"foo.go"}, true},
		{"dir contains", []string{"internal/"}, []string{"internal/api/"}, true},
		{"no overlap", []string{"cmd/"}, []string{"internal/"}, false},
		{"multiple patterns one overlaps", []string{"cmd/", "internal/"}, []string{"pkg/", "internal/api/"}, true},
		{"glob star vs literal", []string{"cmd/*.go"}, []string{"cmd/root.go"}, true},
		{"doublestar glob vs literal", []string{"api/**/*.proto"}, []string{"api/v1/service.proto"}, true},
		{"exact scope both sides", []string{"internal/"}, []string{"internal/"}, true},
		{"empty a", nil, []string{"internal/"}, false},
		{"empty b", []string{"internal/"}, nil, false},
		{"both empty", nil, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, got := scopesOverlap(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("scopesOverlap(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestPatternsOverlap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{"exact match", "internal/api/handler.go", "internal/api/handler.go", true},
		{"parent contains child", "internal", "internal/api", true},
		{"child contains parent", "internal/api", "internal", true},
		{"trailing slash parent", "internal/", "internal/api/", true},
		{"no overlap", "cmd/", "internal/", false},
		{"glob star matches literal", "internal/*.go", "internal/main.go", true},
		{"glob star no match", "internal/*.go", "cmd/main.go", false},
		{"doublestar containment", "internal/**/*.go", "internal/api/handler.go", true},
		{"doublestar different dirs", "internal/**", "cmd/**", false},
		{"sibling dirs", "internal/api/", "internal/loop/", false},
		{"same dir incompatible extensions", "internal/*.go", "internal/*.ts", false},
		{"same dir compatible globs", "internal/*.go", "internal/*.go", true},
		{"same dir wildcard vs extension", "internal/*", "internal/*.go", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := patternsOverlap(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("patternsOverlap(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

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
