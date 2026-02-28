package ui

import (
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/dag"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/nebula"
)

func TestExecutionPlanRender_NoColor(t *testing.T) {
	ep := &nebula.ExecutionPlan{
		Name: "test-nebula",
		Waves: []dag.Wave{
			{Number: 1, NodeIDs: []string{"alpha", "beta"}},
			{Number: 2, NodeIDs: []string{"gamma"}},
		},
		Tracks: []dag.Track{
			{ID: 0, NodeIDs: []string{"alpha", "gamma"}},
			{ID: 1, NodeIDs: []string{"beta"}},
		},
		Contracts: []fabric.PhaseContract{
			{
				PhaseID:  "alpha",
				Produces: []fabric.Entanglement{{Kind: "type", Name: "Foo", Package: "pkg"}},
			},
			{
				PhaseID:  "beta",
				Consumes: []fabric.Entanglement{{Kind: "type", Name: "Foo", Package: "pkg"}},
			},
		},
		Report: &fabric.ContractReport{
			Fulfilled: []fabric.ContractEntry{
				{Consumer: "beta", Producer: "alpha", Entanglement: fabric.Entanglement{Kind: "type", Name: "Foo"}},
			},
		},
		Risks: []nebula.PlanRisk{
			{Severity: "warning", PhaseID: "", Message: "single track warning"},
			{Severity: "info", PhaseID: "gamma", Message: "no contracts"},
		},
		Stats: nebula.PlanStats{
			TotalPhases:        3,
			TotalWaves:         2,
			TotalTracks:        2,
			ParallelFactor:     2,
			FulfilledContracts: 1,
			MissingContracts:   0,
			Conflicts:          0,
			EstimatedCost:      50.0,
		},
	}

	p := New()
	output := captureStderr(func() {
		p.ExecutionPlanRender(ep, true) // noColor=true for testable output
	})

	// Verify key sections are present.
	checks := []string{
		"Observatory: test-nebula",
		"Execution Graph:",
		"Wave 1: alpha, beta",
		"Wave 2: gamma",
		"Tracks: 2 (max parallelism: 2)",
		"Track 0: alpha -> gamma",
		"Track 1: beta",
		"Contracts:",
		"alpha PRODUCES:",
		"type Foo (pkg)",
		"beta CONSUMES:",
		"fulfilled",
		"Risks:",
		"[warning] single track warning",
		"[info] gamma: no contracts",
		"Stats:",
		"Phases: 3 | Waves: 2 | Tracks: 2 | Parallel factor: 2",
		"Contracts: 1 fulfilled, 0 missing, 0 conflicts",
		"Budget cap: $50.00",
	}
	for _, want := range checks {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, output)
		}
	}
}

func TestExecutionPlanRender_WithColor(t *testing.T) {
	ep := &nebula.ExecutionPlan{
		Name: "color-test",
		Waves: []dag.Wave{
			{Number: 1, NodeIDs: []string{"a"}},
		},
		Tracks: []dag.Track{
			{ID: 0, NodeIDs: []string{"a"}},
		},
		Stats: nebula.PlanStats{
			TotalPhases: 1,
			TotalWaves:  1,
			TotalTracks: 1,
		},
	}

	p := New()
	output := captureStderr(func() {
		p.ExecutionPlanRender(ep, false) // noColor=false → ANSI codes present
	})

	// Verify ANSI escape codes are in output.
	if !strings.Contains(output, "\033[") {
		t.Error("expected ANSI escape codes when noColor=false")
	}
}

func TestExecutionPlanRender_NoContracts(t *testing.T) {
	ep := &nebula.ExecutionPlan{
		Name: "no-contracts",
		Waves: []dag.Wave{
			{Number: 1, NodeIDs: []string{"a"}},
		},
		Tracks: []dag.Track{
			{ID: 0, NodeIDs: []string{"a"}},
		},
		Stats: nebula.PlanStats{
			TotalPhases: 1,
			TotalWaves:  1,
			TotalTracks: 1,
		},
	}

	p := New()
	output := captureStderr(func() {
		p.ExecutionPlanRender(ep, true)
	})

	// The Contracts header section (with PRODUCES/CONSUMES) should not appear,
	// but the stats line "Contracts: 0 fulfilled..." is expected.
	if strings.Contains(output, "PRODUCES:") || strings.Contains(output, "CONSUMES:") {
		t.Errorf("expected no contract details for empty contracts, got:\n%s", output)
	}
}

func TestExecutionPlanRender_NoBudget(t *testing.T) {
	ep := &nebula.ExecutionPlan{
		Name: "no-budget",
		Waves: []dag.Wave{
			{Number: 1, NodeIDs: []string{"a"}},
		},
		Tracks: []dag.Track{
			{ID: 0, NodeIDs: []string{"a"}},
		},
		Stats: nebula.PlanStats{
			TotalPhases:   1,
			TotalWaves:    1,
			TotalTracks:   1,
			EstimatedCost: 0,
		},
	}

	p := New()
	output := captureStderr(func() {
		p.ExecutionPlanRender(ep, true)
	})

	if strings.Contains(output, "Budget cap:") {
		t.Errorf("expected no Budget cap line when EstimatedCost is 0, got:\n%s", output)
	}
}

func TestExecutionPlanRender_MissingContract(t *testing.T) {
	ep := &nebula.ExecutionPlan{
		Name: "missing-contract",
		Waves: []dag.Wave{
			{Number: 1, NodeIDs: []string{"consumer"}},
		},
		Tracks: []dag.Track{
			{ID: 0, NodeIDs: []string{"consumer"}},
		},
		Contracts: []fabric.PhaseContract{
			{
				PhaseID:  "consumer",
				Consumes: []fabric.Entanglement{{Kind: "func", Name: "DoStuff"}},
			},
		},
		Report: &fabric.ContractReport{
			// No fulfilled entries — DoStuff is missing.
		},
		Stats: nebula.PlanStats{
			TotalPhases:      1,
			TotalWaves:       1,
			TotalTracks:      1,
			MissingContracts: 1,
		},
	}

	p := New()
	output := captureStderr(func() {
		p.ExecutionPlanRender(ep, true)
	})

	if !strings.Contains(output, "missing") {
		t.Errorf("expected 'missing' for unfulfilled contract, got:\n%s", output)
	}
}

func TestExecutionPlanDiff_NoChanges(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.ExecutionPlanDiff("test", nil, true)
	})

	if !strings.Contains(output, "(no changes)") {
		t.Errorf("expected '(no changes)', got: %s", output)
	}
}

func TestExecutionPlanDiff_WithChanges(t *testing.T) {
	changes := []nebula.PlanChange{
		{Kind: "added", Subject: "new-phase", Detail: "phase added to plan"},
		{Kind: "removed", Subject: "old-phase", Detail: "phase removed from plan"},
		{Kind: "changed", Subject: "waves", Detail: "wave count changed from 2 to 3"},
	}

	p := New()
	output := captureStderr(func() {
		p.ExecutionPlanDiff("diff-test", changes, true)
	})

	if !strings.Contains(output, "Plan diff: diff-test") {
		t.Errorf("missing header, got: %s", output)
	}
	if !strings.Contains(output, "+ new-phase") {
		t.Errorf("missing added change, got: %s", output)
	}
	if !strings.Contains(output, "- old-phase") {
		t.Errorf("missing removed change, got: %s", output)
	}
	if !strings.Contains(output, "~ waves") {
		t.Errorf("missing changed entry, got: %s", output)
	}
}

func TestExecutionPlanDiff_UnknownKind(t *testing.T) {
	changes := []nebula.PlanChange{
		{Kind: "unknown", Subject: "mystery", Detail: "something happened"},
	}

	p := New()
	output := captureStderr(func() {
		p.ExecutionPlanDiff("unknown-test", changes, true)
	})

	if !strings.Contains(output, "? mystery") {
		t.Errorf("expected '? mystery' for unknown kind, got: %s", output)
	}
}

func TestPlanColors_NoColor(t *testing.T) {
	t.Parallel()

	c := planColors(true)
	if c.bold != "" || c.red != "" || c.reset != "" {
		t.Error("expected all empty strings when noColor=true")
	}
}

func TestPlanColors_WithColor(t *testing.T) {
	t.Parallel()

	c := planColors(false)
	if c.bold == "" || c.red == "" || c.reset == "" {
		t.Error("expected non-empty strings when noColor=false")
	}
}

func TestEntanglementPkg(t *testing.T) {
	t.Parallel()

	t.Run("with package", func(t *testing.T) {
		e := fabric.Entanglement{Package: "mypackage"}
		got := entanglementPkg(e)
		if got != " (mypackage)" {
			t.Errorf("entanglementPkg = %q, want %q", got, " (mypackage)")
		}
	})

	t.Run("without package", func(t *testing.T) {
		e := fabric.Entanglement{}
		got := entanglementPkg(e)
		if got != "" {
			t.Errorf("entanglementPkg = %q, want empty", got)
		}
	})
}
