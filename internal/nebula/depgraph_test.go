package nebula

import (
	"errors"
	"strings"
	"testing"
)

func TestInferDependencies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		phases      []PhaseSpec
		wantAdded   int    // minimum number of inferred edges
		wantErr     error  // expected wrapped error, nil for success
		wantErrMsg  string // substring in error message
		wantDep     string // phase ID that should have a new dep
		wantDepOn   string // the dep it should have
		noDep       string // phase ID that should NOT have a specific dep
		noDepOn     string // the dep it should NOT have
		wantWarning string // substring in a warning
	}{
		{
			name: "no overlap disjoint scopes",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"cmd/"}},
				{ID: "b", Title: "B", Scope: []string{"internal/loop/"}},
				{ID: "c", Title: "C", Scope: []string{"internal/nebula/"}},
			},
			wantAdded: 0,
		},
		{
			name: "direct scope overlap adds dependency",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/nebula/"}},
				{ID: "b", Title: "B", Scope: []string{"internal/nebula/"}},
			},
			wantAdded: 1,
			wantDep:   "b",
			wantDepOn: "a",
		},
		{
			name: "scope overlap containment",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/"}},
				{ID: "b", Title: "B", Scope: []string{"internal/nebula/"}},
			},
			wantAdded: 1,
			wantDep:   "b",
			wantDepOn: "a",
		},
		{
			name: "file mention cross-reference adds dependency",
			phases: []PhaseSpec{
				{ID: "types", Title: "Add types", Scope: []string{"internal/nebula/types.go"}},
				{
					ID: "impl", Title: "Use types",
					Body: "## Problem\nSome text\n## Files\n- `internal/nebula/types.go` — use new type\n## Criteria\n",
				},
			},
			wantAdded: 1,
			wantDep:   "impl",
			wantDepOn: "types",
		},
		{
			name: "transitive reduction removes redundant edge",
			phases: []PhaseSpec{
				{ID: "a", Title: "A"},
				{ID: "b", Title: "B", DependsOn: []string{"a"}},
				{ID: "c", Title: "C", DependsOn: []string{"a", "b"}},
			},
			wantAdded:   0,
			wantWarning: "removed",
			noDep:       "c",
			noDepOn:     "a", // c→a is redundant because c→b→a
		},
		{
			name: "cycle detection returns error",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", DependsOn: []string{"b"}},
				{ID: "b", Title: "B", DependsOn: []string{"a"}},
			},
			wantErr:    ErrDependencyCycle,
			wantErrMsg: "cycle",
		},
		{
			name: "blocks expansion creates dependency",
			phases: []PhaseSpec{
				{ID: "setup", Title: "Setup", Blocks: []string{"impl"}},
				{ID: "impl", Title: "Implementation"},
			},
			wantAdded: 1,
			wantDep:   "impl",
			wantDepOn: "setup",
		},
		{
			name: "allow_scope_overlap suppresses inference",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/nebula/"}, AllowScopeOverlap: true},
				{ID: "b", Title: "B", Scope: []string{"internal/nebula/"}},
			},
			wantAdded: 0,
		},
		{
			name: "allow_scope_overlap on second phase suppresses inference",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/nebula/"}},
				{ID: "b", Title: "B", Scope: []string{"internal/nebula/"}, AllowScopeOverlap: true},
			},
			wantAdded: 0,
		},
		{
			name: "existing dependency prevents duplicate scope edge",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/nebula/"}},
				{ID: "b", Title: "B", Scope: []string{"internal/nebula/"}, DependsOn: []string{"a"}},
			},
			wantAdded: 0,
		},
		{
			name: "blocks with unknown target is ignored",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Blocks: []string{"nonexistent"}},
				{ID: "b", Title: "B"},
			},
			wantAdded: 0,
		},
		{
			name:   "no phases produces empty result",
			phases: []PhaseSpec{},
		},
		{
			name: "single phase produces no edges",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/"}},
			},
			wantAdded: 0,
		},
		{
			name: "file mention with no matching scope produces no edge",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"cmd/"}},
				{
					ID: "b", Title: "B",
					Body: "## Files\n- `internal/nebula/types.go` — something\n",
				},
			},
			wantAdded: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inferrer := &DependencyInferrer{Phases: tt.phases}
			result, err := inferrer.InferDependencies()

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error wrapping %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error wrapping %v, got: %v", tt.wantErr, err)
				}
				if tt.wantErrMsg != "" && !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErrMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantAdded > 0 && len(result.Added) < tt.wantAdded {
				t.Errorf("want at least %d added edges, got %d: %+v", tt.wantAdded, len(result.Added), result.Added)
			}
			if tt.wantAdded == 0 && len(result.Added) != 0 {
				t.Errorf("want 0 added edges, got %d: %+v", len(result.Added), result.Added)
			}

			if tt.wantDep != "" {
				found := false
				byID := indexPhases(result.Phases)
				idx := byID[tt.wantDep]
				for _, dep := range result.Phases[idx].DependsOn {
					if dep == tt.wantDepOn {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected phase %q to depend on %q, deps: %v",
						tt.wantDep, tt.wantDepOn, result.Phases[idx].DependsOn)
				}
			}

			if tt.noDep != "" {
				byID := indexPhases(result.Phases)
				idx := byID[tt.noDep]
				for _, dep := range result.Phases[idx].DependsOn {
					if dep == tt.noDepOn {
						t.Errorf("phase %q should NOT depend on %q after transitive reduction, deps: %v",
							tt.noDep, tt.noDepOn, result.Phases[idx].DependsOn)
					}
				}
			}

			if tt.wantWarning != "" {
				found := false
				for _, w := range result.Warnings {
					if strings.Contains(w, tt.wantWarning) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected warning containing %q, got: %v", tt.wantWarning, result.Warnings)
				}
			}
		})
	}
}

func TestExtractFileMentions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "standard files section",
			body: "## Problem\nSome text\n\n## Files\n- `internal/nebula/types.go` — add new type\n- `internal/nebula/validate.go` — update\n\n## Criteria\n- done\n",
			want: []string{"internal/nebula/types.go", "internal/nebula/validate.go"},
		},
		{
			name: "no files section",
			body: "## Problem\nSome text\n\n## Solution\nFix it.\n",
			want: nil,
		},
		{
			name: "asterisk list markers",
			body: "## Files\n* `cmd/root.go` — update\n",
			want: []string{"cmd/root.go"},
		},
		{
			name: "empty files section",
			body: "## Files\n\n## Next\n",
			want: nil,
		},
		{
			name: "files section at end of body",
			body: "## Problem\ntext\n## Files\n- `a.go` — something\n",
			want: []string{"a.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractFileMentions(tt.body)
			if len(got) != len(tt.want) {
				t.Fatalf("extractFileMentions: got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractFileMentions[%d]: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestInferenceResultEdgesHaveReasons(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "a", Title: "A", Scope: []string{"internal/nebula/"}},
		{ID: "b", Title: "B", Scope: []string{"internal/nebula/"}},
	}
	inferrer := &DependencyInferrer{Phases: phases}
	result, err := inferrer.InferDependencies()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, edge := range result.Added {
		if edge.Reason == "" {
			t.Errorf("edge %s → %s has empty reason", edge.From, edge.To)
		}
		if edge.From == "" || edge.To == "" {
			t.Errorf("edge has empty From or To: %+v", edge)
		}
	}
}

func TestInferDependenciesDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "a", Title: "A", Scope: []string{"internal/nebula/"}},
		{ID: "b", Title: "B", Scope: []string{"internal/nebula/"}},
	}
	origBDeps := len(phases[1].DependsOn)
	inferrer := &DependencyInferrer{Phases: phases}
	_, err := inferrer.InferDependencies()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(phases[1].DependsOn) != origBDeps {
		t.Errorf("input phases were mutated: phase b DependsOn = %v", phases[1].DependsOn)
	}
}

func TestBlocksExpansionDuplicateAvoidance(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "setup", Title: "Setup", Blocks: []string{"impl"}},
		{ID: "impl", Title: "Implementation", DependsOn: []string{"setup"}},
	}
	inferrer := &DependencyInferrer{Phases: phases}
	result, err := inferrer.InferDependencies()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "impl" already depends on "setup", so blocks shouldn't add a duplicate.
	byID := indexPhases(result.Phases)
	idx := byID["impl"]
	count := 0
	for _, dep := range result.Phases[idx].DependsOn {
		if dep == "setup" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 dependency on 'setup', got %d: %v",
			count, result.Phases[idx].DependsOn)
	}
}
