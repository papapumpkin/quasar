package checkpoint

import (
	"testing"
	"time"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/papapumpkin/quasar/internal/loop"
)

func TestFromCycleStateToCycleStateRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cs         *loop.CycleState
		phaseID    string
		nebulaName string
		gitSHA     string
	}{
		{
			name:       "empty state",
			cs:         &loop.CycleState{},
			phaseID:    "",
			nebulaName: "",
			gitSHA:     "",
		},
		{
			name: "mid-cycle state with findings",
			cs: &loop.CycleState{
				TaskBeadID:   "bead-123",
				TaskTitle:    "Implement feature X",
				Phase:        loop.PhaseReviewing,
				Cycle:        2,
				MaxCycles:    5,
				TotalCostUSD: 1.23,
				MaxBudgetUSD: 10.0,
				CoderOutput:  "wrote some code",
				ReviewOutput: "found issues",
				LintOutput:   "no lint errors",
				Findings: []loop.ReviewFinding{
					{
						ID:          "f1",
						Severity:    "high",
						Description: "missing error handling",
						Cycle:       2,
						Status:      loop.FindingStatusFound,
					},
				},
				AllFindings: []loop.ReviewFinding{
					{
						ID:          "f0",
						Severity:    "low",
						Description: "naming convention",
						Cycle:       1,
						Status:      loop.FindingStatusFixed,
					},
					{
						ID:          "f1",
						Severity:    "high",
						Description: "missing error handling",
						Cycle:       2,
						Status:      loop.FindingStatusFound,
					},
				},
				BaseCommitSHA: "abc123",
				CycleCommits:  []string{"commit1"},
				ChildBeadIDs:  []string{"child-1", "child-2"},
				Refactored:    true,
			},
			phaseID:    "phase-1",
			nebulaName: "my-nebula",
			gitSHA:     "def456",
		},
		{
			name: "multi-cycle state with commits",
			cs: &loop.CycleState{
				TaskBeadID:    "bead-456",
				TaskTitle:     "Fix bug Y",
				Phase:         loop.PhaseCodeComplete,
				Cycle:         3,
				MaxCycles:     5,
				TotalCostUSD:  5.67,
				MaxBudgetUSD:  50.0,
				CoderOutput:   "final code output",
				ReviewOutput:  "",
				LintOutput:    "all clear",
				BaseCommitSHA: "aaa111",
				CycleCommits:  []string{"commit-a", "commit-b", "commit-c"},
				ChildBeadIDs:  []string{},
				Findings:      nil,
				AllFindings: []loop.ReviewFinding{
					{
						ID:          "f10",
						Severity:    "medium",
						Description: "potential race condition",
						Cycle:       1,
						Status:      loop.FindingStatusFixed,
					},
					{
						ID:          "f11",
						Severity:    "high",
						Description: "nil pointer dereference",
						Cycle:       2,
						Status:      loop.FindingStatusStillPresent,
					},
				},
				Refactored: false,
			},
			phaseID:    "phase-2",
			nebulaName: "bugfix-nebula",
			gitSHA:     "789abc",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// CycleState -> Checkpoint
			cp := FromCycleState(tc.cs, tc.phaseID, tc.nebulaName, tc.gitSHA)

			// Verify metadata fields.
			if cp.Version != Version {
				t.Errorf("Version = %d, want %d", cp.Version, Version)
			}
			if cp.PhaseID != tc.phaseID {
				t.Errorf("PhaseID = %q, want %q", cp.PhaseID, tc.phaseID)
			}
			if cp.NebulaName != tc.nebulaName {
				t.Errorf("NebulaName = %q, want %q", cp.NebulaName, tc.nebulaName)
			}
			if cp.GitSHA != tc.gitSHA {
				t.Errorf("GitSHA = %q, want %q", cp.GitSHA, tc.gitSHA)
			}
			if cp.CreatedAt.IsZero() {
				t.Error("CreatedAt should not be zero")
			}

			// Checkpoint -> CycleState round-trip.
			got := cp.ToCycleState()

			assertCycleStateEqual(t, tc.cs, got)
		})
	}
}

func TestTOMLRoundTrip(t *testing.T) {
	t.Parallel()

	cs := &loop.CycleState{
		TaskBeadID:    "bead-toml",
		TaskTitle:     "TOML round-trip test",
		Phase:         loop.PhaseCoding,
		Cycle:         1,
		MaxCycles:     3,
		TotalCostUSD:  0.5,
		MaxBudgetUSD:  5.0,
		CoderOutput:   "output with special chars: <>&\"'",
		ReviewOutput:  "looks good\nmultiline",
		LintOutput:    "",
		BaseCommitSHA: "sha-toml",
		CycleCommits:  []string{"c1", "c2"},
		ChildBeadIDs:  []string{"child-toml"},
		Findings: []loop.ReviewFinding{
			{
				ID:          "tf1",
				Severity:    "critical",
				Description: "injection vulnerability",
				Cycle:       1,
				Status:      loop.FindingStatusFound,
			},
		},
		AllFindings: []loop.ReviewFinding{
			{
				ID:          "tf1",
				Severity:    "critical",
				Description: "injection vulnerability",
				Cycle:       1,
				Status:      loop.FindingStatusFound,
			},
		},
		Refactored: false,
	}

	cp := FromCycleState(cs, "toml-phase", "toml-nebula", "toml-sha")

	// Serialize to TOML.
	data, err := toml.Marshal(cp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Deserialize back.
	var restored Checkpoint
	if err := toml.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Verify metadata survives.
	if restored.Version != cp.Version {
		t.Errorf("Version = %d, want %d", restored.Version, cp.Version)
	}
	if restored.PhaseID != cp.PhaseID {
		t.Errorf("PhaseID = %q, want %q", restored.PhaseID, cp.PhaseID)
	}
	if restored.NebulaName != cp.NebulaName {
		t.Errorf("NebulaName = %q, want %q", restored.NebulaName, cp.NebulaName)
	}
	if restored.GitSHA != cp.GitSHA {
		t.Errorf("GitSHA = %q, want %q", restored.GitSHA, cp.GitSHA)
	}
	if restored.CreatedAt.IsZero() {
		t.Error("CreatedAt should survive TOML round-trip")
	}

	// Verify CycleState round-trip through TOML.
	got := restored.ToCycleState()
	assertCycleStateEqual(t, cs, got)
}

func TestFindingRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		f    loop.ReviewFinding
	}{
		{
			name: "found finding",
			f: loop.ReviewFinding{
				ID:          "id1",
				Severity:    "high",
				Description: "desc",
				Cycle:       1,
				Status:      loop.FindingStatusFound,
			},
		},
		{
			name: "fixed finding",
			f: loop.ReviewFinding{
				ID:          "id2",
				Severity:    "low",
				Description: "minor issue",
				Cycle:       3,
				Status:      loop.FindingStatusFixed,
			},
		},
		{
			name: "empty finding",
			f:    loop.ReviewFinding{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cpf := FindingFromReview(tc.f)
			got := cpf.ToReviewFinding()

			if got.ID != tc.f.ID {
				t.Errorf("ID = %q, want %q", got.ID, tc.f.ID)
			}
			if got.Severity != tc.f.Severity {
				t.Errorf("Severity = %q, want %q", got.Severity, tc.f.Severity)
			}
			if got.Description != tc.f.Description {
				t.Errorf("Description = %q, want %q", got.Description, tc.f.Description)
			}
			if got.Cycle != tc.f.Cycle {
				t.Errorf("Cycle = %d, want %d", got.Cycle, tc.f.Cycle)
			}
			if got.Status != tc.f.Status {
				t.Errorf("Status = %q, want %q", got.Status, tc.f.Status)
			}
		})
	}
}

func TestFromCycleStateCreatedAtIsPopulated(t *testing.T) {
	t.Parallel()

	before := time.Now()
	cp := FromCycleState(&loop.CycleState{}, "", "", "")
	after := time.Now()

	if cp.CreatedAt.Before(before) || cp.CreatedAt.After(after) {
		t.Errorf("CreatedAt = %v, want between %v and %v", cp.CreatedAt, before, after)
	}
}

func TestFromCycleStateSliceIsolation(t *testing.T) {
	t.Parallel()

	cs := &loop.CycleState{
		CycleCommits: []string{"c1", "c2"},
		ChildBeadIDs: []string{"b1"},
		Findings: []loop.ReviewFinding{
			{ID: "f1", Severity: "high", Description: "issue", Cycle: 1, Status: loop.FindingStatusFound},
		},
	}

	cp := FromCycleState(cs, "", "", "")

	// Mutate originals — checkpoint should not be affected.
	cs.CycleCommits[0] = "MUTATED"
	cs.ChildBeadIDs[0] = "MUTATED"
	cs.Findings[0].Description = "MUTATED"

	if cp.CycleCommits[0] == "MUTATED" {
		t.Error("CycleCommits should be isolated from source")
	}
	if cp.ChildBeadIDs[0] == "MUTATED" {
		t.Error("ChildBeadIDs should be isolated from source")
	}
	if cp.Findings[0].Description == "MUTATED" {
		t.Error("Findings should be isolated from source")
	}
}

// assertCycleStateEqual compares the fields of two CycleState structs that are
// relevant for checkpoint persistence (excluding transient fields).
func assertCycleStateEqual(t *testing.T, want, got *loop.CycleState) {
	t.Helper()

	if got.TaskBeadID != want.TaskBeadID {
		t.Errorf("TaskBeadID = %q, want %q", got.TaskBeadID, want.TaskBeadID)
	}
	if got.TaskTitle != want.TaskTitle {
		t.Errorf("TaskTitle = %q, want %q", got.TaskTitle, want.TaskTitle)
	}
	if got.Phase != want.Phase {
		t.Errorf("Phase = %v, want %v", got.Phase, want.Phase)
	}
	if got.Cycle != want.Cycle {
		t.Errorf("Cycle = %d, want %d", got.Cycle, want.Cycle)
	}
	if got.MaxCycles != want.MaxCycles {
		t.Errorf("MaxCycles = %d, want %d", got.MaxCycles, want.MaxCycles)
	}
	if got.TotalCostUSD != want.TotalCostUSD {
		t.Errorf("TotalCostUSD = %f, want %f", got.TotalCostUSD, want.TotalCostUSD)
	}
	if got.MaxBudgetUSD != want.MaxBudgetUSD {
		t.Errorf("MaxBudgetUSD = %f, want %f", got.MaxBudgetUSD, want.MaxBudgetUSD)
	}
	if got.CoderOutput != want.CoderOutput {
		t.Errorf("CoderOutput = %q, want %q", got.CoderOutput, want.CoderOutput)
	}
	if got.ReviewOutput != want.ReviewOutput {
		t.Errorf("ReviewOutput = %q, want %q", got.ReviewOutput, want.ReviewOutput)
	}
	if got.LintOutput != want.LintOutput {
		t.Errorf("LintOutput = %q, want %q", got.LintOutput, want.LintOutput)
	}
	if got.BaseCommitSHA != want.BaseCommitSHA {
		t.Errorf("BaseCommitSHA = %q, want %q", got.BaseCommitSHA, want.BaseCommitSHA)
	}
	if got.Refactored != want.Refactored {
		t.Errorf("Refactored = %v, want %v", got.Refactored, want.Refactored)
	}

	// Compare slices.
	assertStringSliceEqual(t, "CycleCommits", want.CycleCommits, got.CycleCommits)
	assertStringSliceEqual(t, "ChildBeadIDs", want.ChildBeadIDs, got.ChildBeadIDs)
	assertFindingsEqual(t, "Findings", want.Findings, got.Findings)
	assertFindingsEqual(t, "AllFindings", want.AllFindings, got.AllFindings)
}

func assertStringSliceEqual(t *testing.T, field string, want, got []string) {
	t.Helper()
	// Treat nil and empty as equivalent for round-trip purposes.
	if len(want) == 0 && len(got) == 0 {
		return
	}
	if len(want) != len(got) {
		t.Errorf("%s length = %d, want %d", field, len(got), len(want))
		return
	}
	for i := range want {
		if want[i] != got[i] {
			t.Errorf("%s[%d] = %q, want %q", field, i, got[i], want[i])
		}
	}
}

func assertFindingsEqual(t *testing.T, field string, want, got []loop.ReviewFinding) {
	t.Helper()
	if len(want) == 0 && len(got) == 0 {
		return
	}
	if len(want) != len(got) {
		t.Errorf("%s length = %d, want %d", field, len(got), len(want))
		return
	}
	for i := range want {
		if got[i].ID != want[i].ID {
			t.Errorf("%s[%d].ID = %q, want %q", field, i, got[i].ID, want[i].ID)
		}
		if got[i].Severity != want[i].Severity {
			t.Errorf("%s[%d].Severity = %q, want %q", field, i, got[i].Severity, want[i].Severity)
		}
		if got[i].Description != want[i].Description {
			t.Errorf("%s[%d].Description = %q, want %q", field, i, got[i].Description, want[i].Description)
		}
		if got[i].Cycle != want[i].Cycle {
			t.Errorf("%s[%d].Cycle = %d, want %d", field, i, got[i].Cycle, want[i].Cycle)
		}
		if got[i].Status != want[i].Status {
			t.Errorf("%s[%d].Status = %q, want %q", field, i, got[i].Status, want[i].Status)
		}
	}
}
