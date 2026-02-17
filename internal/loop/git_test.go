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
		sha, err := c.CommitCycle(ctx, "task-1", 3)
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
		want := "quasar: task-1 cycle-3"
		if msg != want {
			t.Errorf("commit message = %q, want %q", msg, want)
		}
	})

	t.Run("returns HEAD SHA when tree is clean", func(t *testing.T) {
		t.Parallel()
		dir := initGitRepo(t)
		c := NewCycleCommitter(context.Background(), dir)

		ctx := context.Background()
		sha, err := c.CommitCycle(ctx, "task-1", 1)
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

func TestNilCycleCommitter(t *testing.T) {
	t.Parallel()

	// A nil *gitCycleCommitter should not panic.
	var c *gitCycleCommitter
	ctx := context.Background()

	t.Run("CommitCycle is no-op", func(t *testing.T) {
		t.Parallel()
		sha, err := c.CommitCycle(ctx, "x", 1)
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
}
