package checkpoint

import (
	"errors"
	"os"
	"path/filepath"
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
				TaskID:       "bead-123",
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
				FilterHistory: []string{"check-lint"},
				Refactored:    true,
			},
			phaseID:    "phase-1",
			nebulaName: "my-nebula",
			gitSHA:     "def456",
		},
		{
			name: "multi-cycle state with commits",
			cs: &loop.CycleState{
				TaskID:        "bead-456",
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
				FilterHistory: []string{"", "check-lint", "check-lint"},
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
		TaskID:        "bead-toml",
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
		FilterHistory: []string{"check-build"},
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
		CycleCommits:  []string{"c1", "c2"},
		FilterHistory: []string{"check-lint", "check-build"},
		Findings: []loop.ReviewFinding{
			{ID: "f1", Severity: "high", Description: "issue", Cycle: 1, Status: loop.FindingStatusFound},
		},
	}

	cp := FromCycleState(cs, "", "", "")

	// Mutate originals — checkpoint should not be affected.
	cs.CycleCommits[0] = "MUTATED"
	cs.FilterHistory[0] = "MUTATED"
	cs.Findings[0].Description = "MUTATED"

	if cp.CycleCommits[0] == "MUTATED" {
		t.Error("CycleCommits should be isolated from source")
	}
	if cp.FilterHistory[0] == "MUTATED" {
		t.Error("FilterHistory should be isolated from source")
	}
	if cp.Findings[0].Description == "MUTATED" {
		t.Error("Findings should be isolated from source")
	}
}

// assertCycleStateEqual compares the fields of two CycleState structs that are
// relevant for checkpoint persistence (excluding transient fields).
func assertCycleStateEqual(t *testing.T, want, got *loop.CycleState) {
	t.Helper()

	if got.TaskID != want.TaskID {
		t.Errorf("TaskID = %q, want %q", got.TaskID, want.TaskID)
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
	assertStringSliceEqual(t, "FilterHistory", want.FilterHistory, got.FilterHistory)
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

// --- Load / LoadAll / Validate / Remove tests ---

// writeCheckpointFile is a test helper that serializes a Checkpoint to TOML
// and writes it to the given path.
func writeCheckpointFile(t *testing.T, path string, cp *Checkpoint) {
	t.Helper()
	data, err := toml.Marshal(cp)
	if err != nil {
		t.Fatalf("marshal checkpoint: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write checkpoint file: %v", err)
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()

	t.Run("no file returns nil nil", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		cp, err := Load(dir, "nonexistent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cp != nil {
			t.Fatalf("expected nil checkpoint, got %+v", cp)
		}
	})

	t.Run("loads existing checkpoint with phase ID", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		want := &Checkpoint{
			Version:    Version,
			PhaseID:    "build-step",
			NebulaName: "test-nebula",
			CreatedAt:  time.Now().Truncate(time.Second),
			GitSHA:     "abc123",
			Cycle:      2,
			MaxCycles:  5,
			Phase:      int(loop.PhaseCoding),
		}
		writeCheckpointFile(t, Path(dir, "build-step"), want)

		got, err := Load(dir, "build-step")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil {
			t.Fatal("expected checkpoint, got nil")
		}
		if got.PhaseID != want.PhaseID {
			t.Errorf("PhaseID = %q, want %q", got.PhaseID, want.PhaseID)
		}
		if got.GitSHA != want.GitSHA {
			t.Errorf("GitSHA = %q, want %q", got.GitSHA, want.GitSHA)
		}
		if got.Cycle != want.Cycle {
			t.Errorf("Cycle = %d, want %d", got.Cycle, want.Cycle)
		}
	})

	t.Run("loads standalone checkpoint (empty phase ID)", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		want := &Checkpoint{
			Version: Version,
			GitSHA:  "standalone-sha",
			Cycle:   1,
			Phase:   int(loop.PhaseReviewing),
		}
		writeCheckpointFile(t, Path(dir, ""), want)

		got, err := Load(dir, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil {
			t.Fatal("expected checkpoint, got nil")
		}
		if got.GitSHA != want.GitSHA {
			t.Errorf("GitSHA = %q, want %q", got.GitSHA, want.GitSHA)
		}
	})

	t.Run("returns error for corrupt TOML", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := Path(dir, "corrupt")
		if err := os.WriteFile(path, []byte("{{not valid toml"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		_, err := Load(dir, "corrupt")
		if err == nil {
			t.Fatal("expected error for corrupt TOML, got nil")
		}
	})
}

func TestLoadAll(t *testing.T) {
	t.Parallel()

	t.Run("empty directory returns empty map", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		result, err := LoadAll(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 0 {
			t.Fatalf("expected empty map, got %d entries", len(result))
		}
	})

	t.Run("nonexistent directory returns nil nil", func(t *testing.T) {
		t.Parallel()
		result, err := LoadAll(filepath.Join(t.TempDir(), "no-such-dir"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Fatalf("expected nil, got %v", result)
		}
	})

	t.Run("discovers multiple checkpoint files", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		cp1 := &Checkpoint{Version: Version, PhaseID: "alpha", GitSHA: "sha-a", Cycle: 1, Phase: int(loop.PhaseCoding)}
		cp2 := &Checkpoint{Version: Version, PhaseID: "beta", GitSHA: "sha-b", Cycle: 2, Phase: int(loop.PhaseReviewing)}
		cp3 := &Checkpoint{Version: Version, GitSHA: "sha-standalone", Cycle: 1, Phase: int(loop.PhaseIdle)}

		writeCheckpointFile(t, Path(dir, "alpha"), cp1)
		writeCheckpointFile(t, Path(dir, "beta"), cp2)
		writeCheckpointFile(t, Path(dir, ""), cp3)

		// Write a non-checkpoint file to ensure it's skipped.
		if err := os.WriteFile(filepath.Join(dir, "other.toml"), []byte("x = 1"), 0o644); err != nil {
			t.Fatalf("write other file: %v", err)
		}

		result, err := LoadAll(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result) != 3 {
			t.Fatalf("expected 3 checkpoints, got %d", len(result))
		}

		if result["alpha"] == nil || result["alpha"].GitSHA != "sha-a" {
			t.Errorf("alpha checkpoint missing or wrong: %+v", result["alpha"])
		}
		if result["beta"] == nil || result["beta"].GitSHA != "sha-b" {
			t.Errorf("beta checkpoint missing or wrong: %+v", result["beta"])
		}
		if result[""] == nil || result[""].GitSHA != "sha-standalone" {
			t.Errorf("standalone checkpoint missing or wrong: %+v", result[""])
		}
	})

	t.Run("skips directories and non-checkpoint files", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		// Create a subdirectory named like a checkpoint.
		if err := os.Mkdir(filepath.Join(dir, "checkpoint.fake.toml"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		// Create a non-matching file.
		if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		result, err := LoadAll(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 0 {
			t.Fatalf("expected 0 checkpoints, got %d", len(result))
		}
	})
}

func TestValidate(t *testing.T) {
	t.Parallel()

	validCP := &Checkpoint{
		Version: Version,
		GitSHA:  "match-sha",
		Cycle:   1,
		Phase:   int(loop.PhaseCoding),
	}

	t.Run("valid checkpoint passes", func(t *testing.T) {
		t.Parallel()
		if err := Validate(validCP, "match-sha"); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("incompatible version", func(t *testing.T) {
		t.Parallel()
		cp := &Checkpoint{Version: 99, GitSHA: "sha", Cycle: 1, Phase: int(loop.PhaseCoding)}
		err := Validate(cp, "sha")
		if err == nil {
			t.Fatal("expected error for incompatible version")
		}
		if !errors.Is(err, ErrIncompatibleVersion) {
			t.Errorf("expected ErrIncompatibleVersion, got %v", err)
		}
	})

	t.Run("git SHA mismatch", func(t *testing.T) {
		t.Parallel()
		cp := &Checkpoint{Version: Version, GitSHA: "old-sha", Cycle: 1, Phase: int(loop.PhaseCoding)}
		err := Validate(cp, "new-sha")
		if err == nil {
			t.Fatal("expected error for SHA mismatch")
		}
		if !errors.Is(err, ErrGitSHAMismatch) {
			t.Errorf("expected ErrGitSHAMismatch, got %v", err)
		}
	})

	t.Run("invalid cycle zero", func(t *testing.T) {
		t.Parallel()
		cp := &Checkpoint{Version: Version, GitSHA: "sha", Cycle: 0, Phase: int(loop.PhaseCoding)}
		err := Validate(cp, "sha")
		if err == nil {
			t.Fatal("expected error for cycle 0")
		}
		if !errors.Is(err, ErrInvalidCheckpoint) {
			t.Errorf("expected ErrInvalidCheckpoint, got %v", err)
		}
	})

	t.Run("invalid negative cycle", func(t *testing.T) {
		t.Parallel()
		cp := &Checkpoint{Version: Version, GitSHA: "sha", Cycle: -1, Phase: int(loop.PhaseCoding)}
		err := Validate(cp, "sha")
		if err == nil {
			t.Fatal("expected error for negative cycle")
		}
		if !errors.Is(err, ErrInvalidCheckpoint) {
			t.Errorf("expected ErrInvalidCheckpoint, got %v", err)
		}
	})

	t.Run("invalid phase too high", func(t *testing.T) {
		t.Parallel()
		cp := &Checkpoint{Version: Version, GitSHA: "sha", Cycle: 1, Phase: 999}
		err := Validate(cp, "sha")
		if err == nil {
			t.Fatal("expected error for invalid phase")
		}
		if !errors.Is(err, ErrInvalidCheckpoint) {
			t.Errorf("expected ErrInvalidCheckpoint, got %v", err)
		}
	})

	t.Run("invalid phase negative", func(t *testing.T) {
		t.Parallel()
		cp := &Checkpoint{Version: Version, GitSHA: "sha", Cycle: 1, Phase: -1}
		err := Validate(cp, "sha")
		if err == nil {
			t.Fatal("expected error for negative phase")
		}
		if !errors.Is(err, ErrInvalidCheckpoint) {
			t.Errorf("expected ErrInvalidCheckpoint, got %v", err)
		}
	})

	t.Run("all valid phases pass", func(t *testing.T) {
		t.Parallel()
		for phase := loop.PhaseIdle; phase <= loop.PhaseError; phase++ {
			cp := &Checkpoint{Version: Version, GitSHA: "sha", Cycle: 1, Phase: int(phase)}
			if err := Validate(cp, "sha"); err != nil {
				t.Errorf("phase %d (%s): unexpected error: %v", phase, phase, err)
			}
		}
	})
}

func TestRemove(t *testing.T) {
	t.Parallel()

	t.Run("removes existing file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		cp := &Checkpoint{Version: Version, Cycle: 1, Phase: int(loop.PhaseCoding)}
		writeCheckpointFile(t, Path(dir, "rm-test"), cp)

		if err := Remove(dir, "rm-test"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify file is gone.
		if _, err := os.Stat(Path(dir, "rm-test")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected file to be removed, got err: %v", err)
		}
	})

	t.Run("no error if file already absent", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := Remove(dir, "nonexistent"); err != nil {
			t.Fatalf("expected nil error for missing file, got %v", err)
		}
	})
}

func TestCheckpointPath(t *testing.T) {
	t.Parallel()

	t.Run("with phase ID", func(t *testing.T) {
		t.Parallel()
		got := Path("/tmp/nebula", "build-step")
		want := filepath.Join("/tmp/nebula", "checkpoint.build-step.toml")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("empty phase ID", func(t *testing.T) {
		t.Parallel()
		got := Path("/tmp/nebula", "")
		want := filepath.Join("/tmp/nebula", "checkpoint.toml")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestExtractPhaseID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{"standalone", "checkpoint.toml", ""},
		{"simple phase", "checkpoint.build.toml", "build"},
		{"hyphenated phase", "checkpoint.build-step.toml", "build-step"},
		{"dotted phase", "checkpoint.a.b.toml", "a.b"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractPhaseID(tc.filename)
			if got != tc.want {
				t.Errorf("extractPhaseID(%q) = %q, want %q", tc.filename, got, tc.want)
			}
		})
	}
}
