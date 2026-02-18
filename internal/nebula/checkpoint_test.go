package nebula

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
)

// mockGitCommitter implements GitCommitter for checkpoint tests.
type mockGitCommitter struct {
	diffLastCommit     string
	diffStatLastCommit string
	diffLastCommitErr  error
	diffStatErr        error
}

func (m *mockGitCommitter) CommitPhase(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockGitCommitter) Diff(_ context.Context) (string, error) {
	return "", nil
}

func (m *mockGitCommitter) DiffLastCommit(_ context.Context) (string, error) {
	return m.diffLastCommit, m.diffLastCommitErr
}

func (m *mockGitCommitter) DiffStatLastCommit(_ context.Context) (string, error) {
	return m.diffStatLastCommit, m.diffStatErr
}

func TestParseDiffStat(t *testing.T) {
	t.Parallel()

	t.Run("parses added files", func(t *testing.T) {
		t.Parallel()
		stat := " scripts/test.sh | 15 +++++++++++++++\n 1 file changed, 15 insertions(+)\n"
		changes := ParseDiffStat(stat)
		if len(changes) != 1 {
			t.Fatalf("expected 1 change, got %d", len(changes))
		}
		fc := changes[0]
		if fc.Path != "scripts/test.sh" {
			t.Errorf("path = %q, want %q", fc.Path, "scripts/test.sh")
		}
		if fc.Operation != "added" {
			t.Errorf("operation = %q, want %q", fc.Operation, "added")
		}
		if fc.LinesAdded != 15 {
			t.Errorf("lines added = %d, want 15", fc.LinesAdded)
		}
		if fc.LinesRemoved != 0 {
			t.Errorf("lines removed = %d, want 0", fc.LinesRemoved)
		}
	})

	t.Run("parses deleted files", func(t *testing.T) {
		t.Parallel()
		stat := " old.txt | 8 --------\n 1 file changed, 8 deletions(-)\n"
		changes := ParseDiffStat(stat)
		if len(changes) != 1 {
			t.Fatalf("expected 1 change, got %d", len(changes))
		}
		fc := changes[0]
		if fc.Path != "old.txt" {
			t.Errorf("path = %q, want %q", fc.Path, "old.txt")
		}
		if fc.Operation != "deleted" {
			t.Errorf("operation = %q, want %q", fc.Operation, "deleted")
		}
		if fc.LinesRemoved != 8 {
			t.Errorf("lines removed = %d, want 8", fc.LinesRemoved)
		}
	})

	t.Run("parses modified files", func(t *testing.T) {
		t.Parallel()
		stat := " main.go | 12 ++++++------\n 1 file changed, 6 insertions(+), 6 deletions(-)\n"
		changes := ParseDiffStat(stat)
		if len(changes) != 1 {
			t.Fatalf("expected 1 change, got %d", len(changes))
		}
		fc := changes[0]
		if fc.Operation != "modified" {
			t.Errorf("operation = %q, want %q", fc.Operation, "modified")
		}
		if fc.LinesAdded != 6 {
			t.Errorf("lines added = %d, want 6", fc.LinesAdded)
		}
		if fc.LinesRemoved != 6 {
			t.Errorf("lines removed = %d, want 6", fc.LinesRemoved)
		}
	})

	t.Run("parses multiple files", func(t *testing.T) {
		t.Parallel()
		stat := ` scripts/test.sh                    | 15 +++++++++++++++
 .github/actions/test/action.yml    | 22 ++++++++++++++++++++++
 2 files changed, 37 insertions(+)
`
		changes := ParseDiffStat(stat)
		if len(changes) != 2 {
			t.Fatalf("expected 2 changes, got %d", len(changes))
		}
		if changes[0].Path != "scripts/test.sh" {
			t.Errorf("first path = %q, want %q", changes[0].Path, "scripts/test.sh")
		}
		if changes[1].Path != ".github/actions/test/action.yml" {
			t.Errorf("second path = %q, want %q", changes[1].Path, ".github/actions/test/action.yml")
		}
	})

	t.Run("handles empty input", func(t *testing.T) {
		t.Parallel()
		changes := ParseDiffStat("")
		if len(changes) != 0 {
			t.Errorf("expected 0 changes, got %d", len(changes))
		}
	})
}

func TestBuildCheckpoint(t *testing.T) {
	t.Parallel()

	nebula := &Nebula{
		Manifest: Manifest{
			Nebula: Info{Name: "CI/CD Pipeline"},
		},
		Phases: []PhaseSpec{
			{ID: "test-script-action", Title: "Test Script Action"},
			{ID: "lint-config", Title: "Lint Config"},
		},
	}

	t.Run("builds checkpoint from result and git", func(t *testing.T) {
		t.Parallel()
		mock := &mockGitCommitter{
			diffLastCommit: "diff --git a/scripts/test.sh b/scripts/test.sh\nnew file mode 100755\n",
			diffStatLastCommit: " scripts/test.sh | 15 +++++++++++++++\n" +
				" 1 file changed, 15 insertions(+)\n",
		}
		result := PhaseRunnerResult{
			TotalCostUSD: 0.12,
			CyclesUsed:   2,
			Report: &agent.ReviewReport{
				Summary: "Clean implementation, follows POSIX conventions",
			},
		}

		cp, err := BuildCheckpoint(context.Background(), mock, "test-script-action", result, nebula)
		if err != nil {
			t.Fatalf("BuildCheckpoint: %v", err)
		}
		if cp.PhaseID != "test-script-action" {
			t.Errorf("PhaseID = %q, want %q", cp.PhaseID, "test-script-action")
		}
		if cp.PhaseTitle != "Test Script Action" {
			t.Errorf("PhaseTitle = %q, want %q", cp.PhaseTitle, "Test Script Action")
		}
		if cp.NebulaName != "CI/CD Pipeline" {
			t.Errorf("NebulaName = %q, want %q", cp.NebulaName, "CI/CD Pipeline")
		}
		if cp.ReviewCycles != 2 {
			t.Errorf("ReviewCycles = %d, want 2", cp.ReviewCycles)
		}
		if cp.CostUSD != 0.12 {
			t.Errorf("CostUSD = %f, want 0.12", cp.CostUSD)
		}
		if cp.ReviewSummary != "Clean implementation, follows POSIX conventions" {
			t.Errorf("ReviewSummary = %q", cp.ReviewSummary)
		}
		if len(cp.FilesChanged) != 1 {
			t.Fatalf("FilesChanged count = %d, want 1", len(cp.FilesChanged))
		}
		if cp.FilesChanged[0].Path != "scripts/test.sh" {
			t.Errorf("FilesChanged[0].Path = %q", cp.FilesChanged[0].Path)
		}
		if !strings.Contains(cp.Diff, "scripts/test.sh") {
			t.Errorf("Diff does not contain expected file path")
		}
	})

	t.Run("handles nil git committer", func(t *testing.T) {
		t.Parallel()
		result := PhaseRunnerResult{
			TotalCostUSD: 0.05,
			CyclesUsed:   1,
		}

		cp, err := BuildCheckpoint(context.Background(), nil, "lint-config", result, nebula)
		if err != nil {
			t.Fatalf("BuildCheckpoint: %v", err)
		}
		if cp.Diff != "" {
			t.Errorf("expected empty diff with nil git, got %q", cp.Diff)
		}
		if len(cp.FilesChanged) != 0 {
			t.Errorf("expected 0 files changed with nil git, got %d", len(cp.FilesChanged))
		}
		if cp.PhaseTitle != "Lint Config" {
			t.Errorf("PhaseTitle = %q, want %q", cp.PhaseTitle, "Lint Config")
		}
	})

	t.Run("handles nil report", func(t *testing.T) {
		t.Parallel()
		mock := &mockGitCommitter{
			diffLastCommit:     "",
			diffStatLastCommit: "",
		}
		result := PhaseRunnerResult{
			TotalCostUSD: 0.03,
			CyclesUsed:   1,
			Report:       nil,
		}

		cp, err := BuildCheckpoint(context.Background(), mock, "test-script-action", result, nebula)
		if err != nil {
			t.Fatalf("BuildCheckpoint: %v", err)
		}
		if cp.ReviewSummary != "" {
			t.Errorf("expected empty ReviewSummary with nil report, got %q", cp.ReviewSummary)
		}
	})
}

func TestRenderCheckpoint(t *testing.T) {
	t.Parallel()

	t.Run("renders full checkpoint", func(t *testing.T) {
		t.Parallel()
		cp := &Checkpoint{
			PhaseID:       "test-script-action",
			PhaseTitle:    "Test Script Action",
			NebulaName:    "CI/CD Pipeline",
			Status:        PhaseStatusDone,
			ReviewCycles:  2,
			CostUSD:       0.12,
			ReviewSummary: "Clean implementation, follows POSIX conventions",
			FilesChanged: []FileChange{
				{Path: "scripts/test.sh", Operation: "added", LinesAdded: 15},
				{Path: ".github/actions/test/action.yml", Operation: "added", LinesAdded: 22},
			},
		}

		var buf bytes.Buffer
		RenderCheckpoint(&buf, cp)
		output := buf.String()

		// Verify key elements are present.
		if !strings.Contains(output, "test-script-action") {
			t.Error("output missing phase ID")
		}
		if !strings.Contains(output, "Test Script Action") {
			t.Error("output missing phase title")
		}
		if !strings.Contains(output, "done") {
			t.Error("output missing status")
		}
		if !strings.Contains(output, "2 review cycles") {
			t.Error("output missing review cycles")
		}
		if !strings.Contains(output, "$0.12") {
			t.Error("output missing cost")
		}
		if !strings.Contains(output, "scripts/test.sh") {
			t.Error("output missing file path")
		}
		if !strings.Contains(output, ".github/actions/test/action.yml") {
			t.Error("output missing second file path")
		}
		if !strings.Contains(output, "Clean implementation, follows POSIX conventions") {
			t.Error("output missing reviewer summary")
		}
		if !strings.Contains(output, "Phase:") {
			t.Error("output missing Phase header")
		}
	})

	t.Run("renders checkpoint without review summary", func(t *testing.T) {
		t.Parallel()
		cp := &Checkpoint{
			PhaseID: "lint",
			Status:  PhaseStatusDone,
			FilesChanged: []FileChange{
				{Path: "main.go", Operation: "modified", LinesAdded: 3, LinesRemoved: 2},
			},
		}

		var buf bytes.Buffer
		RenderCheckpoint(&buf, cp)
		output := buf.String()

		if !strings.Contains(output, "lint") {
			t.Error("output missing phase ID")
		}
		if strings.Contains(output, "Reviewer:") {
			t.Error("output should not contain Reviewer line when summary is empty")
		}
	})

	t.Run("renders deleted files with correct marker", func(t *testing.T) {
		t.Parallel()
		cp := &Checkpoint{
			PhaseID: "cleanup",
			Status:  PhaseStatusDone,
			FilesChanged: []FileChange{
				{Path: "old.txt", Operation: "deleted", LinesRemoved: 10},
			},
		}

		var buf bytes.Buffer
		RenderCheckpoint(&buf, cp)
		output := buf.String()

		if !strings.Contains(output, "- ") {
			t.Error("output missing deletion marker")
		}
		if !strings.Contains(output, "old.txt") {
			t.Error("output missing deleted file path")
		}
	})

	t.Run("renders single cycle correctly", func(t *testing.T) {
		t.Parallel()
		cp := &Checkpoint{
			PhaseID:      "setup",
			Status:       PhaseStatusDone,
			ReviewCycles: 1,
		}

		var buf bytes.Buffer
		RenderCheckpoint(&buf, cp)
		output := buf.String()

		if !strings.Contains(output, "1 review cycle") {
			t.Error("output should use singular 'cycle'")
		}
		if strings.Contains(output, "1 review cycles") {
			t.Error("output should not use plural 'cycles' for 1")
		}
	})
}
