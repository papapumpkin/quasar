package nebula

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initTestRepo creates a temporary git repo with an initial commit.
// Returns the repo directory.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()

	// Initialize repo.
	run(ctx, t, dir, "git", "init")
	run(ctx, t, dir, "git", "config", "user.email", "test@test.com")
	run(ctx, t, dir, "git", "config", "user.name", "Test")

	// Create initial commit so HEAD exists.
	initial := filepath.Join(dir, "README.md")
	if err := os.WriteFile(initial, []byte("# test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run(ctx, t, dir, "git", "add", "-A")
	run(ctx, t, dir, "git", "commit", "-m", "initial")

	return dir
}

// run executes a command in the given directory and fails the test on error.
func run(ctx context.Context, t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

// commitCount returns the number of commits in the repo.
func commitCount(ctx context.Context, t *testing.T, dir string) int {
	t.Helper()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-list", "--count", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-list: %v", err)
	}
	s := strings.TrimSpace(string(out))
	var n int
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

// lastCommitMessage returns the message of the most recent commit.
func lastCommitMessage(ctx context.Context, t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "log", "-1", "--format=%s")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestNewGitCommitter(t *testing.T) {
	t.Run("returns committer for valid repo", func(t *testing.T) {
		dir := initTestRepo(t)
		gc := NewGitCommitter(context.Background(), dir)
		if gc == nil {
			t.Fatal("expected non-nil committer for valid git repo")
		}
	})

	t.Run("returns nil for non-repo directory", func(t *testing.T) {
		dir := t.TempDir() // not a git repo
		gc := NewGitCommitter(context.Background(), dir)
		if gc != nil {
			t.Fatal("expected nil committer for non-repo directory")
		}
	})
}

func TestGitCommitter_CommitPhase(t *testing.T) {
	t.Run("creates commit with correct message", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()
		gc := NewGitCommitter(ctx, dir)
		if gc == nil {
			t.Fatal("expected non-nil committer")
		}

		// Create a file to trigger a commit.
		if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\n"), 0644); err != nil {
			t.Fatal(err)
		}

		if err := gc.CommitPhase(ctx, "CI/CD Pipeline", "test-script-action", "Run CI test scripts"); err != nil {
			t.Fatalf("CommitPhase: %v", err)
		}

		msg := lastCommitMessage(ctx, t, dir)
		want := "CI/CD Pipeline/test-script-action: Run CI test scripts"
		if msg != want {
			t.Errorf("commit message = %q, want %q", msg, want)
		}
	})

	t.Run("no-op on clean working tree", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()
		gc := NewGitCommitter(ctx, dir)
		if gc == nil {
			t.Fatal("expected non-nil committer")
		}

		before := commitCount(ctx, t, dir)
		if err := gc.CommitPhase(ctx, "test", "phase-1", "Test phase one"); err != nil {
			t.Fatalf("CommitPhase: %v", err)
		}
		after := commitCount(ctx, t, dir)

		if after != before {
			t.Errorf("commit count changed from %d to %d on clean tree", before, after)
		}
	})

	t.Run("truncates long phase title", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()
		gc := NewGitCommitter(ctx, dir)
		if gc == nil {
			t.Fatal("expected non-nil committer")
		}

		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0644); err != nil {
			t.Fatal(err)
		}

		longTitle := strings.Repeat("a", 200)
		if err := gc.CommitPhase(ctx, "neb", "ph", longTitle); err != nil {
			t.Fatalf("CommitPhase: %v", err)
		}

		msg := lastCommitMessage(ctx, t, dir)
		// prefix = "neb/ph: " (8 chars), so title budget = 72, truncated = 69 + "..."
		if len(msg) > 80 {
			t.Errorf("commit message too long: %d chars: %q", len(msg), msg)
		}
		if !strings.HasSuffix(msg, "...") {
			t.Errorf("expected truncated message to end with '...', got %q", msg)
		}
	})

	t.Run("no panic when prefix nearly fills 80 chars", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()
		gc := NewGitCommitter(ctx, dir)
		if gc == nil {
			t.Fatal("expected non-nil committer")
		}

		if err := os.WriteFile(filepath.Join(dir, "g.txt"), []byte("y\n"), 0644); err != nil {
			t.Fatal(err)
		}

		// Use a very long nebulaName+phaseID so the prefix is ~78 chars.
		// "aaaa...a/bbbb...b: " leaves maxTitle <= 3.
		longNebula := strings.Repeat("a", 40)
		longPhase := strings.Repeat("b", 36)
		// prefix = 40 + "/" + 36 + ": " = 79 chars, maxTitle = 1
		if err := gc.CommitPhase(ctx, longNebula, longPhase, "Some title"); err != nil {
			t.Fatalf("CommitPhase: %v", err)
		}

		// Should not panic; title is kept as-is (no truncation when maxTitle <= 3).
		msg := lastCommitMessage(ctx, t, dir)
		if !strings.Contains(msg, "Some title") {
			t.Errorf("expected full title in message when prefix is long, got %q", msg)
		}
	})

	t.Run("stages and commits all changes", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()
		gc := NewGitCommitter(ctx, dir)
		if gc == nil {
			t.Fatal("expected non-nil committer")
		}

		// Create multiple files.
		for _, name := range []string{"a.txt", "b.txt", "subdir/c.txt"} {
			path := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("content\n"), 0644); err != nil {
				t.Fatal(err)
			}
		}

		if err := gc.CommitPhase(ctx, "multi-file", "phase-2", "Stage and commit all changes"); err != nil {
			t.Fatalf("CommitPhase: %v", err)
		}

		// Verify working tree is clean after commit.
		statusCmd := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain")
		out, err := statusCmd.Output()
		if err != nil {
			t.Fatalf("git status: %v", err)
		}
		if len(strings.TrimSpace(string(out))) > 0 {
			t.Errorf("working tree not clean after commit: %s", out)
		}
	})
}

func TestGitCommitter_Diff(t *testing.T) {
	t.Run("returns empty diff on clean tree", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()
		gc := NewGitCommitter(ctx, dir)
		if gc == nil {
			t.Fatal("expected non-nil committer")
		}

		diff, err := gc.Diff(ctx)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		if diff != "" {
			t.Errorf("expected empty diff, got %q", diff)
		}
	})

	t.Run("returns diff for modified files", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()
		gc := NewGitCommitter(ctx, dir)
		if gc == nil {
			t.Fatal("expected non-nil committer")
		}

		// Modify the existing file.
		readme := filepath.Join(dir, "README.md")
		if err := os.WriteFile(readme, []byte("# updated\n"), 0644); err != nil {
			t.Fatal(err)
		}

		diff, err := gc.Diff(ctx)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		if !strings.Contains(diff, "updated") {
			t.Errorf("diff does not contain expected change: %s", diff)
		}
	})
}
