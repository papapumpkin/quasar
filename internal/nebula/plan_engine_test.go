package nebula

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/dag"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// testNebula creates a minimal Nebula from phase specs with defaults.
func testNebula(name string, phases []PhaseSpec) *Nebula {
	return &Nebula{
		Manifest: Manifest{
			Nebula: Info{Name: name},
			Execution: Execution{
				MaxWorkers: 2,
			},
		},
		Phases: phases,
	}
}

func TestPlanEngine_Plan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		nebula         *Nebula
		wantWaves      int
		wantTracks     int
		wantPhases     int
		wantParallel   int
		wantErrSubstr  string
		checkRisks     func(t *testing.T, risks []PlanRisk)
		checkContracts func(t *testing.T, ep *ExecutionPlan)
	}{
		{
			name: "simple linear chain",
			nebula: testNebula("linear", []PhaseSpec{
				{ID: "a", Title: "Phase A", Priority: 1, Body: "## Problem\nCreate Foo type\n## Files\n- `internal/foo.go` — create Foo"},
				{ID: "b", Title: "Phase B", Priority: 2, DependsOn: []string{"a"}, Body: "## Problem\nUse Foo\n## Files\n- `internal/bar.go` — use Foo"},
				{ID: "c", Title: "Phase C", Priority: 3, DependsOn: []string{"b"}, Body: "## Problem\nFinalize\n## Files\n- `internal/baz.go` — finalize"},
			}),
			wantWaves:    3,
			wantTracks:   1,
			wantPhases:   3,
			wantParallel: 1,
		},
		{
			name: "diamond dependency",
			nebula: testNebula("diamond", []PhaseSpec{
				{ID: "root", Title: "Root", Priority: 1, Body: "## Problem\nSetup"},
				{ID: "left", Title: "Left", Priority: 2, DependsOn: []string{"root"}, Body: "## Problem\nLeft branch"},
				{ID: "right", Title: "Right", Priority: 2, DependsOn: []string{"root"}, Body: "## Problem\nRight branch"},
				{ID: "join", Title: "Join", Priority: 3, DependsOn: []string{"left", "right"}, Body: "## Problem\nMerge"},
			}),
			wantWaves:    3,
			wantTracks:   1, // diamond has cross-dependencies so single track
			wantPhases:   4,
			wantParallel: 2, // left and right are parallel in wave 2
		},
		{
			name: "parallel tracks",
			nebula: testNebula("parallel", []PhaseSpec{
				{ID: "track1-a", Title: "T1A", Priority: 1, Body: "## Problem\nTrack 1 start"},
				{ID: "track1-b", Title: "T1B", Priority: 2, DependsOn: []string{"track1-a"}, Body: "## Problem\nTrack 1 end"},
				{ID: "track2-a", Title: "T2A", Priority: 1, Body: "## Problem\nTrack 2 start"},
				{ID: "track2-b", Title: "T2B", Priority: 2, DependsOn: []string{"track2-a"}, Body: "## Problem\nTrack 2 end"},
			}),
			wantWaves:    2,
			wantTracks:   2,
			wantPhases:   4,
			wantParallel: 2,
			checkRisks: func(t *testing.T, risks []PlanRisk) {
				t.Helper()
				// Two independent tracks, no issues expected.
				for _, r := range risks {
					if r.Severity == "error" {
						t.Errorf("unexpected error risk: %s", r.Message)
					}
				}
			},
		},
		{
			name: "missing contracts",
			nebula: testNebula("missing", []PhaseSpec{
				{ID: "consumer", Title: "Consumer", Priority: 1, DependsOn: []string{"producer"},
					Body: "## Problem\nNeeds FooService type\n\nUse `FooService` from producer.\n## Files\n- `internal/svc.go` — use FooService"},
				{ID: "producer", Title: "Producer", Priority: 1,
					Body: "## Problem\nCreate something else\n## Files\n- `internal/other.go` — create Other"},
			}),
			wantWaves:  2,
			wantPhases: 2,
			checkContracts: func(t *testing.T, ep *ExecutionPlan) {
				t.Helper()
				// The consumer depends on producer, but producer doesn't
				// produce FooService — that should show up in some form.
				// The contract resolution depends on what the static scanner finds.
				if ep.Report == nil {
					t.Error("expected non-nil contract report")
				}
			},
		},
		{
			name: "scope conflicts",
			nebula: testNebula("scope-conflict", []PhaseSpec{
				{ID: "alpha", Title: "Alpha", Priority: 1,
					Scope: []string{"internal/shared/**"},
					Body:  "## Problem\nModify shared code"},
				{ID: "beta", Title: "Beta", Priority: 1,
					Scope: []string{"internal/shared/**"},
					Body:  "## Problem\nAlso modify shared code"},
			}),
			wantWaves:    1,
			wantPhases:   2,
			wantParallel: 2,
			checkRisks: func(t *testing.T, risks []PlanRisk) {
				t.Helper()
				found := false
				for _, r := range risks {
					if r.Severity == "error" && strings.Contains(r.Message, "scope overlap") {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected error risk for scope overlap")
				}
			},
		},
		{
			name: "scope overlap allowed",
			nebula: testNebula("scope-allowed", []PhaseSpec{
				{ID: "alpha", Title: "Alpha", Priority: 1,
					Scope:             []string{"internal/shared/**"},
					AllowScopeOverlap: true,
					Body:              "## Problem\nModify shared code"},
				{ID: "beta", Title: "Beta", Priority: 1,
					Scope: []string{"internal/shared/**"},
					Body:  "## Problem\nAlso modify shared code"},
			}),
			wantWaves:  1,
			wantPhases: 2,
			checkRisks: func(t *testing.T, risks []PlanRisk) {
				t.Helper()
				for _, r := range risks {
					if r.Severity == "error" && strings.Contains(r.Message, "scope overlap") {
						t.Error("should not have scope overlap error when allow_scope_overlap is true")
					}
				}
			},
		},
		{
			name: "single track warning",
			nebula: func() *Nebula {
				n := testNebula("bottleneck", []PhaseSpec{
					{ID: "a", Title: "A", Priority: 1, Body: "## Problem\nStart"},
					{ID: "b", Title: "B", Priority: 2, DependsOn: []string{"a"}, Body: "## Problem\nMiddle"},
					{ID: "c", Title: "C", Priority: 3, DependsOn: []string{"b"}, Body: "## Problem\nEnd"},
				})
				n.Manifest.Execution.MaxWorkers = 4
				return n
			}(),
			wantWaves:    3,
			wantTracks:   1,
			wantPhases:   3,
			wantParallel: 1,
			checkRisks: func(t *testing.T, risks []PlanRisk) {
				t.Helper()
				found := false
				for _, r := range risks {
					if r.Severity == "warning" && strings.Contains(r.Message, "single execution track") {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected warning for single track with max_workers > 1")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Use a temp directory as work dir for the scanner.
			tmpDir := t.TempDir()
			pe := &PlanEngine{
				Scanner: &fabric.StaticScanner{WorkDir: tmpDir},
			}

			ep, err := pe.Plan(tt.nebula)
			if tt.wantErrSubstr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrSubstr)
				}
				if !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErrSubstr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ep.Name != tt.nebula.Manifest.Nebula.Name {
				t.Errorf("name = %q, want %q", ep.Name, tt.nebula.Manifest.Nebula.Name)
			}
			if tt.wantWaves > 0 && ep.Stats.TotalWaves != tt.wantWaves {
				t.Errorf("waves = %d, want %d", ep.Stats.TotalWaves, tt.wantWaves)
			}
			if tt.wantTracks > 0 && ep.Stats.TotalTracks != tt.wantTracks {
				t.Errorf("tracks = %d, want %d", ep.Stats.TotalTracks, tt.wantTracks)
			}
			if tt.wantPhases > 0 && ep.Stats.TotalPhases != tt.wantPhases {
				t.Errorf("phases = %d, want %d", ep.Stats.TotalPhases, tt.wantPhases)
			}
			if tt.wantParallel > 0 && ep.Stats.ParallelFactor != tt.wantParallel {
				t.Errorf("parallel factor = %d, want %d", ep.Stats.ParallelFactor, tt.wantParallel)
			}

			// Verify contracts are populated.
			if len(ep.Contracts) != len(tt.nebula.Phases) {
				t.Errorf("contracts count = %d, want %d", len(ep.Contracts), len(tt.nebula.Phases))
			}

			// Verify report is non-nil.
			if ep.Report == nil {
				t.Error("expected non-nil contract report")
			}

			// Verify impact order has all phases.
			if len(ep.ImpactOrder) != len(tt.nebula.Phases) {
				t.Errorf("impact order count = %d, want %d", len(ep.ImpactOrder), len(tt.nebula.Phases))
			}

			if tt.checkRisks != nil {
				tt.checkRisks(t, ep.Risks)
			}
			if tt.checkContracts != nil {
				tt.checkContracts(t, ep)
			}
		})
	}
}

func TestExecutionPlan_SaveLoad(t *testing.T) {
	t.Parallel()

	ep := &ExecutionPlan{
		Name: "test-nebula",
		Risks: []PlanRisk{
			{Severity: "error", PhaseID: "p1", Message: "missing producer"},
		},
		Stats: PlanStats{
			TotalPhases:    3,
			TotalWaves:     2,
			TotalTracks:    1,
			ParallelFactor: 2,
		},
	}

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.plan.json")

	if err := ep.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify the file exists and is valid JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading saved file: %v", err)
	}
	if !json.Valid(data) {
		t.Error("saved file is not valid JSON")
	}

	loaded, err := LoadPlan(path)
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}

	if loaded.Name != ep.Name {
		t.Errorf("loaded name = %q, want %q", loaded.Name, ep.Name)
	}
	if loaded.Stats.TotalPhases != ep.Stats.TotalPhases {
		t.Errorf("loaded phases = %d, want %d", loaded.Stats.TotalPhases, ep.Stats.TotalPhases)
	}
	if loaded.Stats.TotalWaves != ep.Stats.TotalWaves {
		t.Errorf("loaded waves = %d, want %d", loaded.Stats.TotalWaves, ep.Stats.TotalWaves)
	}
	if len(loaded.Risks) != 1 {
		t.Fatalf("loaded risks count = %d, want 1", len(loaded.Risks))
	}
	if loaded.Risks[0].Severity != "error" {
		t.Errorf("loaded risk severity = %q, want %q", loaded.Risks[0].Severity, "error")
	}
}

func TestLoadPlan_NotFound(t *testing.T) {
	t.Parallel()

	_, err := LoadPlan("/nonexistent/path.json")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestLoadPlan_InvalidJSON(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bad.json")
	if err := os.WriteFile(path, []byte("{invalid}"), 0o644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	_, err := LoadPlan(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestDiff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		old        *ExecutionPlan
		new        *ExecutionPlan
		wantCount  int
		wantKinds  map[string]bool
		checkDiffs func(t *testing.T, changes []PlanChange)
	}{
		{
			name: "no changes",
			old: &ExecutionPlan{
				Waves:     []dag.Wave{{Number: 1, NodeIDs: []string{"a"}}},
				Contracts: []fabric.PhaseContract{{PhaseID: "a"}},
				Stats:     PlanStats{TotalWaves: 1, TotalTracks: 1},
			},
			new: &ExecutionPlan{
				Waves:     []dag.Wave{{Number: 1, NodeIDs: []string{"a"}}},
				Contracts: []fabric.PhaseContract{{PhaseID: "a"}},
				Stats:     PlanStats{TotalWaves: 1, TotalTracks: 1},
			},
			wantCount: 0,
		},
		{
			name: "phase added",
			old: &ExecutionPlan{
				Waves:     []dag.Wave{{Number: 1, NodeIDs: []string{"a"}}},
				Contracts: []fabric.PhaseContract{{PhaseID: "a"}},
				Stats:     PlanStats{TotalWaves: 1, TotalTracks: 1},
			},
			new: &ExecutionPlan{
				Waves:     []dag.Wave{{Number: 1, NodeIDs: []string{"a", "b"}}},
				Contracts: []fabric.PhaseContract{{PhaseID: "a"}, {PhaseID: "b"}},
				Stats:     PlanStats{TotalWaves: 1, TotalTracks: 1},
			},
			wantKinds: map[string]bool{"added": true},
			checkDiffs: func(t *testing.T, changes []PlanChange) {
				t.Helper()
				found := false
				for _, c := range changes {
					if c.Kind == "added" && c.Subject == "b" {
						found = true
					}
				}
				if !found {
					t.Error("expected 'added' change for phase 'b'")
				}
			},
		},
		{
			name: "phase removed",
			old: &ExecutionPlan{
				Waves:     []dag.Wave{{Number: 1, NodeIDs: []string{"a"}}, {Number: 2, NodeIDs: []string{"b"}}},
				Contracts: []fabric.PhaseContract{{PhaseID: "a"}, {PhaseID: "b"}},
				Stats:     PlanStats{TotalWaves: 2, TotalTracks: 1},
			},
			new: &ExecutionPlan{
				Waves:     []dag.Wave{{Number: 1, NodeIDs: []string{"a"}}},
				Contracts: []fabric.PhaseContract{{PhaseID: "a"}},
				Stats:     PlanStats{TotalWaves: 1, TotalTracks: 1},
			},
			wantKinds: map[string]bool{"removed": true, "changed": true},
			checkDiffs: func(t *testing.T, changes []PlanChange) {
				t.Helper()
				found := false
				for _, c := range changes {
					if c.Kind == "removed" && c.Subject == "b" {
						found = true
					}
				}
				if !found {
					t.Error("expected 'removed' change for phase 'b'")
				}
			},
		},
		{
			name: "stats changed",
			old: &ExecutionPlan{
				Waves:     []dag.Wave{{Number: 1, NodeIDs: []string{"a"}}},
				Contracts: []fabric.PhaseContract{{PhaseID: "a"}},
				Stats:     PlanStats{TotalWaves: 1, TotalTracks: 1, FulfilledContracts: 2, Conflicts: 0},
			},
			new: &ExecutionPlan{
				Waves:     []dag.Wave{{Number: 1, NodeIDs: []string{"a"}}},
				Contracts: []fabric.PhaseContract{{PhaseID: "a"}},
				Stats:     PlanStats{TotalWaves: 2, TotalTracks: 2, FulfilledContracts: 3, Conflicts: 1},
			},
			checkDiffs: func(t *testing.T, changes []PlanChange) {
				t.Helper()
				subjects := make(map[string]bool)
				for _, c := range changes {
					subjects[c.Subject] = true
				}
				for _, want := range []string{"waves", "tracks", "contracts", "conflicts"} {
					if !subjects[want] {
						t.Errorf("expected change for subject %q", want)
					}
				}
			},
		},
		{
			name: "risk severity changes",
			old: &ExecutionPlan{
				Waves:     []dag.Wave{{Number: 1, NodeIDs: []string{"a"}}},
				Contracts: []fabric.PhaseContract{{PhaseID: "a"}},
				Risks:     []PlanRisk{{Severity: "error", PhaseID: "a", Message: "bad"}},
				Stats:     PlanStats{TotalWaves: 1, TotalTracks: 1},
			},
			new: &ExecutionPlan{
				Waves:     []dag.Wave{{Number: 1, NodeIDs: []string{"a"}}},
				Contracts: []fabric.PhaseContract{{PhaseID: "a"}},
				Risks:     []PlanRisk{},
				Stats:     PlanStats{TotalWaves: 1, TotalTracks: 1},
			},
			checkDiffs: func(t *testing.T, changes []PlanChange) {
				t.Helper()
				found := false
				for _, c := range changes {
					if c.Subject == "risks/error" {
						found = true
					}
				}
				if !found {
					t.Error("expected change for risks/error")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			changes := Diff(tt.old, tt.new)

			if tt.wantCount > 0 && len(changes) != tt.wantCount {
				t.Errorf("change count = %d, want %d", len(changes), tt.wantCount)
			}
			if tt.wantCount == 0 && tt.wantKinds == nil && tt.checkDiffs == nil && len(changes) != 0 {
				t.Errorf("expected no changes, got %d", len(changes))
			}

			if tt.wantKinds != nil {
				kindsSeen := make(map[string]bool)
				for _, c := range changes {
					kindsSeen[c.Kind] = true
				}
				for kind := range tt.wantKinds {
					if !kindsSeen[kind] {
						t.Errorf("expected change of kind %q", kind)
					}
				}
			}

			if tt.checkDiffs != nil {
				tt.checkDiffs(t, changes)
			}
		})
	}
}

func TestPlanStats_Accuracy(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	pe := &PlanEngine{
		Scanner: &fabric.StaticScanner{WorkDir: tmpDir},
	}

	n := &Nebula{
		Manifest: Manifest{
			Nebula: Info{Name: "stats-test"},
			Execution: Execution{
				MaxWorkers:   3,
				MaxBudgetUSD: 100.0,
			},
		},
		Phases: []PhaseSpec{
			{ID: "a", Title: "A", Priority: 1, Body: "## Problem\nDo A"},
			{ID: "b", Title: "B", Priority: 2, Body: "## Problem\nDo B"},
			{ID: "c", Title: "C", Priority: 3, DependsOn: []string{"a"}, Body: "## Problem\nDo C"},
		},
	}

	ep, err := pe.Plan(n)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if ep.Stats.TotalPhases != 3 {
		t.Errorf("total phases = %d, want 3", ep.Stats.TotalPhases)
	}

	// Waves: a,b in wave 1 (no deps), c in wave 2 (depends on a).
	if ep.Stats.TotalWaves != 2 {
		t.Errorf("total waves = %d, want 2", ep.Stats.TotalWaves)
	}

	// Parallel factor should be 2 (wave 1 has a and b).
	if ep.Stats.ParallelFactor != 2 {
		t.Errorf("parallel factor = %d, want 2", ep.Stats.ParallelFactor)
	}

	// No per-phase budgets, so estimated cost comes from manifest.
	if ep.Stats.EstimatedCost != 100.0 {
		t.Errorf("estimated cost = %f, want 100.0", ep.Stats.EstimatedCost)
	}

	// Contracts should have 0 conflicts for this simple case.
	if ep.Stats.Conflicts != 0 {
		t.Errorf("conflicts = %d, want 0", ep.Stats.Conflicts)
	}
}

func TestPlanEngine_ImpactOrder(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	pe := &PlanEngine{
		Scanner: &fabric.StaticScanner{WorkDir: tmpDir},
	}

	// Root has highest impact (everything depends on it), leaf has lowest.
	n := testNebula("impact", []PhaseSpec{
		{ID: "root", Title: "Root", Priority: 1, Body: "## Problem\nRoot"},
		{ID: "mid", Title: "Mid", Priority: 2, DependsOn: []string{"root"}, Body: "## Problem\nMid"},
		{ID: "leaf", Title: "Leaf", Priority: 3, DependsOn: []string{"mid"}, Body: "## Problem\nLeaf"},
	})

	ep, err := pe.Plan(n)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if len(ep.ImpactOrder) != 3 {
		t.Fatalf("impact order length = %d, want 3", len(ep.ImpactOrder))
	}

	// Verify all phases are present in impact order.
	seen := make(map[string]bool)
	for _, id := range ep.ImpactOrder {
		seen[id] = true
	}
	for _, want := range []string{"root", "mid", "leaf"} {
		if !seen[want] {
			t.Errorf("missing phase %q in impact order", want)
		}
	}

	// Leaf should be last or near-last — it has no dependents.
	if ep.ImpactOrder[len(ep.ImpactOrder)-1] != "leaf" {
		t.Errorf("last in impact order = %q, want %q", ep.ImpactOrder[len(ep.ImpactOrder)-1], "leaf")
	}
}
