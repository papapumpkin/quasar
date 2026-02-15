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
	run(t, ctx, dir, "git", "init")
	run(t, ctx, dir, "git", "config", "user.email", "test@test.com")
	run(t, ctx, dir, "git", "config", "user.name", "Test")

	// Create initial commit so HEAD exists.
	initial := filepath.Join(dir, "README.md")
	if err := os.WriteFile(initial, []byte("# test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run(t, ctx, dir, "git", "add", "-A")
	run(t, ctx, dir, "git", "commit", "-m", "initial")

	return dir
}

// run executes a command in the given directory and fails the test on error.
func run(t *testing.T, ctx context.Context, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

// commitCount returns the number of commits in the repo.
func commitCount(t *testing.T, ctx context.Context, dir string) int {
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
func lastCommitMessage(t *testing.T, ctx context.Context, dir string) string {
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

		if err := gc.CommitPhase(ctx, "CI/CD Pipeline", "test-script-action"); err != nil {
			t.Fatalf("CommitPhase: %v", err)
		}

		msg := lastCommitMessage(t, ctx, dir)
		want := "nebula(CI/CD Pipeline): test-script-action"
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

		before := commitCount(t, ctx, dir)
		if err := gc.CommitPhase(ctx, "test", "phase-1"); err != nil {
			t.Fatalf("CommitPhase: %v", err)
		}
		after := commitCount(t, ctx, dir)

		if after != before {
			t.Errorf("commit count changed from %d to %d on clean tree", before, after)
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

		if err := gc.CommitPhase(ctx, "multi-file", "phase-2"); err != nil {
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
