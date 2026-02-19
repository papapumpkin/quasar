package nebula

import (
	"context"
	"fmt"
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

func TestPostCompletionResult_Summary(t *testing.T) {
	t.Parallel()

	t.Run("success summary", func(t *testing.T) {
		t.Parallel()
		r := &PostCompletionResult{PushBranch: "nebula/my-test", CheckoutBranch: "main"}
		s := r.Summary()
		if !strings.Contains(s, "Pushed to origin/nebula/my-test") {
			t.Errorf("expected push success in summary, got %q", s)
		}
		if !strings.Contains(s, "Checked out main") {
			t.Errorf("expected checkout success in summary, got %q", s)
		}
	})

	t.Run("push error summary", func(t *testing.T) {
		t.Parallel()
		r := &PostCompletionResult{
			PushBranch: "nebula/fail",
			PushErr:    fmt.Errorf("no remote"),
		}
		s := r.Summary()
		if !strings.Contains(s, "Push failed") {
			t.Errorf("expected push failure in summary, got %q", s)
		}
		if !strings.Contains(s, "no remote") {
			t.Errorf("expected error detail in summary, got %q", s)
		}
	})

	t.Run("checkout error summary", func(t *testing.T) {
		t.Parallel()
		r := &PostCompletionResult{
			PushBranch:     "nebula/fail",
			CheckoutBranch: "main",
			CheckoutErr:    fmt.Errorf("dirty worktree"),
		}
		s := r.Summary()
		if !strings.Contains(s, "Checkout main failed") {
			t.Errorf("expected checkout failure in summary, got %q", s)
		}
		if !strings.Contains(s, "dirty worktree") {
			t.Errorf("expected error detail in summary, got %q", s)
		}
	})

	t.Run("commit error summary", func(t *testing.T) {
		t.Parallel()
		r := &PostCompletionResult{
			PushBranch:     "nebula/fail",
			CommitErr:      fmt.Errorf("git add: permission denied"),
			CheckoutBranch: "main",
		}
		s := r.Summary()
		if !strings.Contains(s, "Commit failed") {
			t.Errorf("expected commit failure in summary, got %q", s)
		}
		if !strings.Contains(s, "permission denied") {
			t.Errorf("expected error detail in summary, got %q", s)
		}
	})

	t.Run("checkout with master branch", func(t *testing.T) {
		t.Parallel()
		r := &PostCompletionResult{
			PushBranch:     "nebula/test",
			CheckoutBranch: "master",
		}
		s := r.Summary()
		if !strings.Contains(s, "Checked out master") {
			t.Errorf("expected 'Checked out master' in summary, got %q", s)
		}
	})
}

func TestCommitRemaining(t *testing.T) {
	t.Run("commits uncommitted changes", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()

		// Create a branch and switch to it.
		run(ctx, t, dir, "git", "checkout", "-b", "nebula/test-commit")

		// Create an uncommitted file.
		if err := os.WriteFile(filepath.Join(dir, "uncommitted.txt"), []byte("data\n"), 0644); err != nil {
			t.Fatal(err)
		}

		before := commitCount(ctx, t, dir)
		if err := commitRemaining(ctx, dir, "nebula/test-commit"); err != nil {
			t.Fatalf("commitRemaining: %v", err)
		}
		after := commitCount(ctx, t, dir)

		if after != before+1 {
			t.Errorf("expected commit count to increase by 1, got %d -> %d", before, after)
		}

		msg := lastCommitMessage(ctx, t, dir)
		if !strings.Contains(msg, "nebula: final changes on nebula/test-commit") {
			t.Errorf("unexpected commit message: %q", msg)
		}
	})

	t.Run("no-op on clean working tree", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()

		before := commitCount(ctx, t, dir)
		if err := commitRemaining(ctx, dir, "nebula/clean"); err != nil {
			t.Fatalf("commitRemaining: %v", err)
		}
		after := commitCount(ctx, t, dir)

		if after != before {
			t.Errorf("expected no new commit on clean tree, got %d -> %d", before, after)
		}
	})
}

func TestPostCompletion(t *testing.T) {
	t.Run("push fails without remote", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()

		// Create a nebula branch.
		run(ctx, t, dir, "git", "checkout", "-b", "nebula/no-remote")

		result := PostCompletion(ctx, dir, "nebula/no-remote")

		// Push should fail because there's no remote.
		if result.PushErr == nil {
			t.Error("expected push error when no remote exists")
		}
		if result.PushBranch != "nebula/no-remote" {
			t.Errorf("expected PushBranch='nebula/no-remote', got %q", result.PushBranch)
		}
	})

	t.Run("checkout main fails when main does not exist", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()

		// The default branch in initTestRepo might be "master" depending on git config.
		// Create a nebula branch from whatever default branch.
		run(ctx, t, dir, "git", "checkout", "-b", "nebula/no-main")

		result := PostCompletion(ctx, dir, "nebula/no-main")

		// Checkout main may fail if the default branch is "master".
		// We just verify the result is populated.
		if result.PushBranch != "nebula/no-main" {
			t.Errorf("expected PushBranch='nebula/no-main', got %q", result.PushBranch)
		}
	})

	t.Run("checkout succeeds when main exists", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()

		// Ensure "main" branch exists. initTestRepo may create "master".
		// Rename to main.
		run(ctx, t, dir, "git", "branch", "-M", "main")

		// Create nebula branch.
		run(ctx, t, dir, "git", "checkout", "-b", "nebula/checkout-test")

		result := PostCompletion(ctx, dir, "nebula/checkout-test")

		if result.CheckoutErr != nil {
			t.Errorf("expected checkout to succeed: %v", result.CheckoutErr)
		}
		if result.CheckoutBranch != "main" {
			t.Errorf("expected CheckoutBranch='main', got %q", result.CheckoutBranch)
		}

		// Verify we're on main now.
		current := currentBranchHelper(ctx, t, dir)
		if current != "main" {
			t.Errorf("expected to be on main after PostCompletion, got %q", current)
		}
	})

	t.Run("checkout succeeds when master is default", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()

		// Ensure "master" branch exists.
		run(ctx, t, dir, "git", "branch", "-M", "master")

		// Create nebula branch.
		run(ctx, t, dir, "git", "checkout", "-b", "nebula/master-test")

		result := PostCompletion(ctx, dir, "nebula/master-test")

		if result.CheckoutErr != nil {
			t.Errorf("expected checkout to succeed: %v", result.CheckoutErr)
		}
		if result.CheckoutBranch != "master" {
			t.Errorf("expected CheckoutBranch='master', got %q", result.CheckoutBranch)
		}

		// Verify we're on master now.
		current := currentBranchHelper(ctx, t, dir)
		if current != "master" {
			t.Errorf("expected to be on master after PostCompletion, got %q", current)
		}
	})
}

func TestGitCommitter_DiffRange(t *testing.T) {
	t.Run("returns diff between two commits", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()
		gc := NewGitCommitter(ctx, dir)
		if gc == nil {
			t.Fatal("expected non-nil committer")
		}

		// Record the initial commit SHA.
		baseSHA := headSHA(ctx, t, dir)

		// Create a file and commit.
		if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("hello\n"), 0644); err != nil {
			t.Fatal(err)
		}
		run(ctx, t, dir, "git", "add", "-A")
		run(ctx, t, dir, "git", "commit", "-m", "add new.txt")
		headSHA1 := headSHA(ctx, t, dir)

		diff, err := gc.DiffRange(ctx, baseSHA, headSHA1)
		if err != nil {
			t.Fatalf("DiffRange: %v", err)
		}
		if !strings.Contains(diff, "new.txt") {
			t.Errorf("DiffRange output should contain 'new.txt', got %q", diff)
		}
		if !strings.Contains(diff, "hello") {
			t.Errorf("DiffRange output should contain 'hello', got %q", diff)
		}
	})

	t.Run("returns empty for identical SHAs", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()
		gc := NewGitCommitter(ctx, dir)
		if gc == nil {
			t.Fatal("expected non-nil committer")
		}

		sha := headSHA(ctx, t, dir)
		diff, err := gc.DiffRange(ctx, sha, sha)
		if err != nil {
			t.Fatalf("DiffRange: %v", err)
		}
		if diff != "" {
			t.Errorf("expected empty diff for same SHA, got %q", diff)
		}
	})

	t.Run("returns error for invalid SHA", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()
		gc := NewGitCommitter(ctx, dir)
		if gc == nil {
			t.Fatal("expected non-nil committer")
		}

		_, err := gc.DiffRange(ctx, "0000000000000000000000000000000000000000", "HEAD")
		if err == nil {
			t.Fatal("expected error for invalid base SHA")
		}
	})

	t.Run("nil receiver returns empty string", func(t *testing.T) {
		var gc *gitCommitter
		diff, err := gc.DiffRange(context.Background(), "abc", "def")
		if err != nil {
			t.Fatalf("nil DiffRange: %v", err)
		}
		if diff != "" {
			t.Errorf("nil DiffRange returned %q, want empty", diff)
		}
	})
}

func TestGitCommitter_DiffStatRange(t *testing.T) {
	t.Run("returns stat between two commits", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()
		gc := NewGitCommitter(ctx, dir)
		if gc == nil {
			t.Fatal("expected non-nil committer")
		}

		baseSHA := headSHA(ctx, t, dir)

		// Create a file and commit.
		if err := os.WriteFile(filepath.Join(dir, "stats.txt"), []byte("data\n"), 0644); err != nil {
			t.Fatal(err)
		}
		run(ctx, t, dir, "git", "add", "-A")
		run(ctx, t, dir, "git", "commit", "-m", "add stats.txt")
		headSHA1 := headSHA(ctx, t, dir)

		stat, err := gc.DiffStatRange(ctx, baseSHA, headSHA1)
		if err != nil {
			t.Fatalf("DiffStatRange: %v", err)
		}
		if !strings.Contains(stat, "stats.txt") {
			t.Errorf("DiffStatRange output should contain 'stats.txt', got %q", stat)
		}
		if !strings.Contains(stat, "1 file changed") {
			t.Errorf("DiffStatRange output should contain '1 file changed', got %q", stat)
		}
	})

	t.Run("returns error for invalid SHA", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()
		gc := NewGitCommitter(ctx, dir)
		if gc == nil {
			t.Fatal("expected non-nil committer")
		}

		_, err := gc.DiffStatRange(ctx, "0000000000000000000000000000000000000000", "HEAD")
		if err == nil {
			t.Fatal("expected error for invalid base SHA")
		}
	})

	t.Run("nil receiver returns empty string", func(t *testing.T) {
		var gc *gitCommitter
		stat, err := gc.DiffStatRange(context.Background(), "abc", "def")
		if err != nil {
			t.Fatalf("nil DiffStatRange: %v", err)
		}
		if stat != "" {
			t.Errorf("nil DiffStatRange returned %q, want empty", stat)
		}
	})
}

func TestGitCommitter_ResetTo(t *testing.T) {
	t.Run("resets working tree to target SHA", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()
		gc := NewGitCommitter(ctx, dir)
		if gc == nil {
			t.Fatal("expected non-nil committer")
		}

		// Record initial SHA.
		baseSHA := headSHA(ctx, t, dir)

		// Create a file and commit.
		if err := os.WriteFile(filepath.Join(dir, "reset.txt"), []byte("content\n"), 0644); err != nil {
			t.Fatal(err)
		}
		run(ctx, t, dir, "git", "add", "-A")
		run(ctx, t, dir, "git", "commit", "-m", "add reset.txt")

		// Verify file exists.
		if _, err := os.Stat(filepath.Join(dir, "reset.txt")); err != nil {
			t.Fatalf("reset.txt should exist before reset: %v", err)
		}

		// Reset to base.
		if err := gc.ResetTo(ctx, baseSHA); err != nil {
			t.Fatalf("ResetTo: %v", err)
		}

		// Verify file is gone and HEAD matches base.
		if _, err := os.Stat(filepath.Join(dir, "reset.txt")); !os.IsNotExist(err) {
			t.Error("reset.txt should not exist after reset to initial commit")
		}
		if headSHA(ctx, t, dir) != baseSHA {
			t.Errorf("HEAD after reset = %q, want %q", headSHA(ctx, t, dir), baseSHA)
		}
	})

	t.Run("returns error for invalid SHA", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()
		gc := NewGitCommitter(ctx, dir)
		if gc == nil {
			t.Fatal("expected non-nil committer")
		}

		err := gc.ResetTo(ctx, "0000000000000000000000000000000000000000")
		if err == nil {
			t.Fatal("expected error for invalid SHA")
		}
	})

	t.Run("verifies branch before reset", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()

		sha := headSHA(ctx, t, dir)

		// Create a committer expecting branch "feature" but repo is on default branch.
		gc := NewGitCommitterWithBranch(ctx, dir, "feature")
		err := gc.ResetTo(ctx, sha)
		if err == nil {
			t.Fatal("expected branch mismatch error")
		}
		if !strings.Contains(err.Error(), "branch mismatch") {
			t.Errorf("expected 'branch mismatch' in error, got %q", err)
		}
	})

	t.Run("nil receiver is no-op", func(t *testing.T) {
		var gc *gitCommitter
		err := gc.ResetTo(context.Background(), "abc123")
		if err != nil {
			t.Fatalf("nil ResetTo: %v", err)
		}
	})
}

// headSHA returns the current HEAD SHA in the given repo.
func headSHA(ctx context.Context, t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestDetectDefaultBranch(t *testing.T) {
	t.Run("detects main branch", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()

		// Rename default branch to "main".
		run(ctx, t, dir, "git", "branch", "-M", "main")

		got := detectDefaultBranch(ctx, dir)
		if got != "main" {
			t.Errorf("expected 'main', got %q", got)
		}
	})

	t.Run("detects master branch", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()

		// Rename default branch to "master".
		run(ctx, t, dir, "git", "branch", "-M", "master")

		got := detectDefaultBranch(ctx, dir)
		if got != "master" {
			t.Errorf("expected 'master', got %q", got)
		}
	})

	t.Run("falls back to main when neither exists", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()

		// Rename default branch to something unusual.
		run(ctx, t, dir, "git", "branch", "-M", "develop")

		// Switch to another branch so "develop" can be checked.
		run(ctx, t, dir, "git", "checkout", "-b", "nebula/test")

		got := detectDefaultBranch(ctx, dir)
		// Neither "main" nor "master" exists, so should fall back to "main".
		if got != "main" {
			t.Errorf("expected fallback to 'main', got %q", got)
		}
	})
}
