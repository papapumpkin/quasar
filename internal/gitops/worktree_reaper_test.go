package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// porcelainRunner returns a fakeRunner that replies to `worktree list
// --porcelain` with listing and succeeds for `worktree prune`.
func porcelainRunner(listing string) *fakeRunner {
	return &fakeRunner{fn: func(args []string) ([]byte, []byte, error) {
		if len(args) >= 2 && args[0] == "worktree" && args[1] == "list" {
			return []byte(listing), nil, nil
		}
		return nil, nil, nil
	}}
}

// makeWorktreeDir creates a populated worktree directory with the given mtime
// and returns its path.
func makeWorktreeDir(t *testing.T, mtime time.Time) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("contents"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Chtimes(dir, mtime, mtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	return dir
}

func TestWorktreeName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		wt   Worktree
		want string
	}{
		{Worktree{Branch: "refs/heads/quasar/run-123"}, "run-123"},
		{Worktree{Branch: "refs/heads/quasar/feat/sub"}, "feat/sub"},
		{Worktree{Path: "/tmp/wt-abc"}, "wt-abc"}, // detached: basename fallback
	}
	for _, c := range cases {
		if got := c.wt.Name(); got != c.want {
			t.Errorf("Name() = %q, want %q", got, c.want)
		}
	}
}

func TestParseWorktrees(t *testing.T) {
	t.Parallel()
	dir := makeWorktreeDir(t, time.Now())
	out := strings.Join([]string{
		"worktree /repo",
		"branch refs/heads/main",
		"",
		"worktree " + dir,
		"branch refs/heads/quasar/run-1",
		"prunable gitdir file points to non-existent location",
		"",
	}, "\n")

	got := parseWorktrees(out)
	if len(got) != 2 {
		t.Fatalf("parsed %d worktrees, want 2", len(got))
	}
	if got[1].Branch != "refs/heads/quasar/run-1" || !got[1].Prunable {
		t.Errorf("second entry = %+v, want quasar/run-1 prunable", got[1])
	}
}

func TestReap(t *testing.T) {
	ctx := context.Background()
	maxAge := time.Hour
	now := time.Now()

	t.Run("ignores non-quasar worktrees", func(t *testing.T) {
		listing := "worktree /repo\nbranch refs/heads/main\n"
		r := porcelainRunner(listing)
		reaper := NewWorktreeReaperWithRunner(r.run)
		report, err := reaper.Reap(ctx, "/repo", maxAge, now, nil, false)
		if err != nil {
			t.Fatalf("Reap: %v", err)
		}
		if len(report.Removed) != 0 || report.Kept != 0 {
			t.Errorf("report = %+v, want non-quasar worktree skipped entirely", report)
		}
	})

	t.Run("keeps a protected worktree regardless of age", func(t *testing.T) {
		dir := makeWorktreeDir(t, now.Add(-48*time.Hour)) // old
		listing := "worktree " + dir + "\nbranch refs/heads/quasar/run-live\n"
		r := porcelainRunner(listing)
		reaper := NewWorktreeReaperWithRunner(r.run)
		isProtected := func(name string) bool { return name == "run-live" }

		report, err := reaper.Reap(ctx, "/repo", maxAge, now, isProtected, false)
		if err != nil {
			t.Fatalf("Reap: %v", err)
		}
		if len(report.Removed) != 0 || report.Kept != 1 {
			t.Errorf("report = %+v, want protected worktree kept", report)
		}
		if _, err := os.Stat(dir); err != nil {
			t.Error("protected worktree directory was removed")
		}
	})

	t.Run("keeps a young, non-prunable worktree", func(t *testing.T) {
		dir := makeWorktreeDir(t, now) // fresh
		listing := "worktree " + dir + "\nbranch refs/heads/quasar/run-young\n"
		r := porcelainRunner(listing)
		reaper := NewWorktreeReaperWithRunner(r.run)
		report, err := reaper.Reap(ctx, "/repo", maxAge, now, nil, false)
		if err != nil {
			t.Fatalf("Reap: %v", err)
		}
		if len(report.Removed) != 0 || report.Kept != 1 {
			t.Errorf("report = %+v, want young worktree kept", report)
		}
	})

	t.Run("removes an old, unprotected worktree and prunes", func(t *testing.T) {
		dir := makeWorktreeDir(t, now.Add(-48*time.Hour))
		listing := "worktree " + dir + "\nbranch refs/heads/quasar/run-old\n"
		r := porcelainRunner(listing)
		reaper := NewWorktreeReaperWithRunner(r.run)

		report, err := reaper.Reap(ctx, "/repo", maxAge, now, nil, false)
		if err != nil {
			t.Fatalf("Reap: %v", err)
		}
		if len(report.Removed) != 1 || report.Removed[0] != dir {
			t.Errorf("Removed = %v, want [%s]", report.Removed, dir)
		}
		if report.ReclaimedBytes == 0 {
			t.Error("ReclaimedBytes = 0, want the freed file bytes")
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Error("worktree directory still present after reap")
		}
		// The reaper must prune git's administrative record after removal.
		var pruned bool
		for _, call := range r.calls {
			if len(call) >= 2 && call[0] == "worktree" && call[1] == "prune" {
				pruned = true
			}
		}
		if !pruned {
			t.Error("git worktree prune was not invoked")
		}
	})

	t.Run("removes a prunable worktree even when young", func(t *testing.T) {
		dir := makeWorktreeDir(t, now) // fresh, but git says prunable
		listing := "worktree " + dir + "\nbranch refs/heads/quasar/run-gone\nprunable gitdir gone\n"
		r := porcelainRunner(listing)
		reaper := NewWorktreeReaperWithRunner(r.run)
		report, err := reaper.Reap(ctx, "/repo", maxAge, now, nil, false)
		if err != nil {
			t.Fatalf("Reap: %v", err)
		}
		if len(report.Removed) != 1 {
			t.Errorf("Removed = %v, want the prunable worktree", report.Removed)
		}
	})

	t.Run("dry run reports but touches no disk or git state", func(t *testing.T) {
		dir := makeWorktreeDir(t, now.Add(-48*time.Hour))
		listing := "worktree " + dir + "\nbranch refs/heads/quasar/run-old\n"
		r := porcelainRunner(listing)
		reaper := NewWorktreeReaperWithRunner(r.run)

		report, err := reaper.Reap(ctx, "/repo", maxAge, now, nil, true)
		if err != nil {
			t.Fatalf("Reap: %v", err)
		}
		if len(report.Removed) != 1 {
			t.Errorf("dry run Removed = %v, want 1 (reported)", report.Removed)
		}
		if _, err := os.Stat(dir); err != nil {
			t.Error("dry run removed the worktree directory")
		}
		for _, call := range r.calls {
			if len(call) >= 2 && call[1] == "prune" {
				t.Error("dry run invoked git worktree prune")
			}
		}
	})
}
