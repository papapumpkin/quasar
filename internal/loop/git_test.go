package loop

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initGitRepo creates a temporary git repo with an initial commit.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init")
	run("config", "user.name", "test")
	run("config", "user.email", "test@test.com")
	run("commit", "--allow-empty", "-m", "initial")
	return dir
}

func TestNewCycleCommitter(t *testing.T) {
	t.Parallel()

	t.Run("returns committer for git repo", func(t *testing.T) {
		t.Parallel()
		dir := initGitRepo(t)
		c := NewCycleCommitter(context.Background(), dir)
		if c == nil {
			t.Fatal("expected non-nil CycleCommitter for git repo")
		}
	})

	t.Run("returns nil for non-git directory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		c := NewCycleCommitter(context.Background(), dir)
		if c != nil {
			t.Fatal("expected nil CycleCommitter for non-git directory")
		}
	})
}

func TestCommitCycle(t *testing.T) {
	t.Parallel()

	t.Run("creates commit with expected message", func(t *testing.T) {
		t.Parallel()
		dir := initGitRepo(t)
		c := NewCycleCommitter(context.Background(), dir)

		// Create a file so there's something to commit.
		if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}

		ctx := context.Background()
		sha, err := c.CommitCycle(ctx, "task-1", 3, "Fix status bar wrapping")
		if err != nil {
			t.Fatalf("CommitCycle: %v", err)
		}
		if len(sha) < 7 {
			t.Fatalf("expected valid SHA, got %q", sha)
		}

		// Verify commit message.
		cmd := exec.Command("git", "-C", dir, "log", "-1", "--format=%s")
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		msg := strings.TrimSpace(string(out))
		want := "task-1/cycle-3: Fix status bar wrapping"
		if msg != want {
			t.Errorf("commit message = %q, want %q", msg, want)
		}
	})

	t.Run("returns HEAD SHA when tree is clean", func(t *testing.T) {
		t.Parallel()
		dir := initGitRepo(t)
		c := NewCycleCommitter(context.Background(), dir)

		ctx := context.Background()
		sha, err := c.CommitCycle(ctx, "task-1", 1, "No changes")
		if err != nil {
			t.Fatalf("CommitCycle on clean tree: %v", err)
		}

		headSHA, err := c.HeadSHA(ctx)
		if err != nil {
			t.Fatalf("HeadSHA: %v", err)
		}
		if sha != headSHA {
			t.Errorf("CommitCycle SHA = %q, HeadSHA = %q; want equal", sha, headSHA)
		}
	})
}

func TestHeadSHA(t *testing.T) {
	t.Parallel()
	dir := initGitRepo(t)
	c := NewCycleCommitter(context.Background(), dir)

	ctx := context.Background()
	sha, err := c.HeadSHA(ctx)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("expected 40-char SHA, got %q (len %d)", sha, len(sha))
	}
}

func TestDiffRange(t *testing.T) {
	t.Parallel()

	t.Run("returns diff between two commits", func(t *testing.T) {
		t.Parallel()
		dir := initGitRepo(t)
		c := NewCycleCommitter(context.Background(), dir)
		ctx := context.Background()

		// Record the initial commit SHA.
		baseSHA, err := c.HeadSHA(ctx)
		if err != nil {
			t.Fatalf("HeadSHA: %v", err)
		}

		// Create a file and commit.
		if err := os.WriteFile(filepath.Join(dir, "diff.txt"), []byte("content\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		sha, err := c.CommitCycle(ctx, "test", 1, "add diff.txt")
		if err != nil {
			t.Fatalf("CommitCycle: %v", err)
		}

		diff, err := c.DiffRange(ctx, baseSHA, sha)
		if err != nil {
			t.Fatalf("DiffRange: %v", err)
		}
		if !strings.Contains(diff, "diff.txt") {
			t.Errorf("DiffRange output should contain 'diff.txt', got %q", diff)
		}
		if !strings.Contains(diff, "content") {
			t.Errorf("DiffRange output should contain 'content', got %q", diff)
		}
	})

	t.Run("returns empty for identical SHAs", func(t *testing.T) {
		t.Parallel()
		dir := initGitRepo(t)
		c := NewCycleCommitter(context.Background(), dir)
		ctx := context.Background()

		sha, err := c.HeadSHA(ctx)
		if err != nil {
			t.Fatalf("HeadSHA: %v", err)
		}

		diff, err := c.DiffRange(ctx, sha, sha)
		if err != nil {
			t.Fatalf("DiffRange: %v", err)
		}
		if diff != "" {
			t.Errorf("expected empty diff for same SHA, got %q", diff)
		}
	})

	t.Run("returns error for invalid SHA", func(t *testing.T) {
		t.Parallel()
		dir := initGitRepo(t)
		c := NewCycleCommitter(context.Background(), dir)

		_, err := c.DiffRange(context.Background(), "0000000000000000000000000000000000000000", "HEAD")
		if err == nil {
			t.Fatal("expected error for invalid base SHA")
		}
	})
}

func TestResetTo(t *testing.T) {
	t.Parallel()

	t.Run("resets working tree to target SHA", func(t *testing.T) {
		t.Parallel()
		dir := initGitRepo(t)
		c := NewCycleCommitter(context.Background(), dir)
		ctx := context.Background()

		// Record the initial SHA.
		baseSHA, err := c.HeadSHA(ctx)
		if err != nil {
			t.Fatalf("HeadSHA: %v", err)
		}

		// Create a file and commit.
		if err := os.WriteFile(filepath.Join(dir, "reset.txt"), []byte("content\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err = c.CommitCycle(ctx, "test", 1, "add reset.txt")
		if err != nil {
			t.Fatalf("CommitCycle: %v", err)
		}

		// Verify file exists.
		if _, err := os.Stat(filepath.Join(dir, "reset.txt")); err != nil {
			t.Fatalf("reset.txt should exist before reset: %v", err)
		}

		// Reset to the initial commit.
		if err := c.ResetTo(ctx, baseSHA); err != nil {
			t.Fatalf("ResetTo: %v", err)
		}

		// Verify file is gone and HEAD matches base SHA.
		if _, err := os.Stat(filepath.Join(dir, "reset.txt")); !os.IsNotExist(err) {
			t.Error("reset.txt should not exist after reset to initial commit")
		}
		headAfter, err := c.HeadSHA(ctx)
		if err != nil {
			t.Fatalf("HeadSHA after reset: %v", err)
		}
		if headAfter != baseSHA {
			t.Errorf("HEAD after reset = %q, want %q", headAfter, baseSHA)
		}
	})

	t.Run("returns error for invalid SHA", func(t *testing.T) {
		t.Parallel()
		dir := initGitRepo(t)
		c := NewCycleCommitter(context.Background(), dir)

		err := c.ResetTo(context.Background(), "0000000000000000000000000000000000000000")
		if err == nil {
			t.Fatal("expected error for invalid SHA")
		}
	})

	t.Run("returns error for non-ancestor SHA", func(t *testing.T) {
		t.Parallel()
		dir := initGitRepo(t)
		ctx := context.Background()

		// Name the current branch so we can return to it.
		cmd := exec.Command("git", "-C", dir, "branch", "-M", "main")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git branch -M main: %v\n%s", err, out)
		}

		// Create an orphan branch with its own commit.
		cmd = exec.Command("git", "-C", dir, "checkout", "--orphan", "orphan")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git checkout --orphan: %v\n%s", err, out)
		}
		if err := os.WriteFile(filepath.Join(dir, "orphan.txt"), []byte("orphan\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			{"add", "-A"},
			{"commit", "-m", "orphan commit"},
		} {
			cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
			}
		}
		// Get orphan SHA.
		cmd = exec.Command("git", "-C", dir, "rev-parse", "HEAD")
		orphanOut, err := cmd.Output()
		if err != nil {
			t.Fatalf("rev-parse orphan HEAD: %v", err)
		}
		orphanSHA := strings.TrimSpace(string(orphanOut))

		// Switch back to the original branch.
		cmd = exec.Command("git", "-C", dir, "checkout", "main")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git checkout main: %v\n%s", err, out)
		}

		c := NewCycleCommitter(ctx, dir)
		err = c.ResetTo(ctx, orphanSHA)
		if err == nil {
			t.Fatal("expected error for non-ancestor SHA")
		}
	})

	t.Run("verifies branch before reset", func(t *testing.T) {
		t.Parallel()
		dir := initGitRepo(t)
		ctx := context.Background()

		// Record current HEAD.
		cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		sha := strings.TrimSpace(string(out))

		// Create a committer expecting branch "feature" but repo is on default branch.
		c := NewCycleCommitterWithBranch(ctx, dir, "feature")
		err = c.ResetTo(ctx, sha)
		if err == nil {
			t.Fatal("expected branch mismatch error")
		}
		if !strings.Contains(err.Error(), "branch mismatch") {
			t.Errorf("expected 'branch mismatch' in error, got %q", err)
		}
	})
}

func TestNilCycleCommitter(t *testing.T) {
	t.Parallel()

	// A nil *gitCycleCommitter should not panic.
	var c *gitCycleCommitter
	ctx := context.Background()

	t.Run("CommitCycle is no-op", func(t *testing.T) {
		t.Parallel()
		sha, err := c.CommitCycle(ctx, "x", 1, "test")
		if err != nil {
			t.Fatalf("nil CommitCycle: %v", err)
		}
		if sha != "" {
			t.Errorf("nil CommitCycle returned sha %q, want empty", sha)
		}
	})

	t.Run("HeadSHA is no-op", func(t *testing.T) {
		t.Parallel()
		sha, err := c.HeadSHA(ctx)
		if err != nil {
			t.Fatalf("nil HeadSHA: %v", err)
		}
		if sha != "" {
			t.Errorf("nil HeadSHA returned sha %q, want empty", sha)
		}
	})

	t.Run("DiffRange is no-op", func(t *testing.T) {
		t.Parallel()
		diff, err := c.DiffRange(ctx, "abc", "def")
		if err != nil {
			t.Fatalf("nil DiffRange: %v", err)
		}
		if diff != "" {
			t.Errorf("nil DiffRange returned %q, want empty", diff)
		}
	})

	t.Run("ResetTo is no-op", func(t *testing.T) {
		t.Parallel()
		err := c.ResetTo(ctx, "abc123")
		if err != nil {
			t.Fatalf("nil ResetTo: %v", err)
		}
	})
}
