package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsureWorktree exercises the real git worktree path: a per-nebula worktree
// is created on its quasar/<nebula> branch, the call is idempotent, and a NEW
// file commits cleanly inside the worktree (worktree isolation + git add -A).
func TestEnsureWorktree(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	mustGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	mustGit("init", "-q")
	// Configure an identity IN the repo so commits made through gitops.Client
	// (which does not inject GIT_AUTHOR_* env) succeed on CI, where git has no
	// global user.name/user.email. Worktrees share the repo's config.
	mustGit("config", "user.email", "test@quasar.local")
	mustGit("config", "user.name", "Quasar Test")
	// Real Quasar repos gitignore .quasar/ (the fabric db lives there), so the
	// build worktrees under .quasar/worktrees/ never show in the operator's
	// git status. Mirror that here.
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".quasar/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "-A")
	mustGit("commit", "-qm", "init")

	c := New(dir)
	ctx := context.Background()

	if got := c.BranchFor("neb-1"); got != "quasar/nebula-neb-1" {
		t.Errorf("BranchFor = %q, want quasar/nebula-neb-1", got)
	}

	path, err := c.EnsureWorktree(ctx, "neb-1")
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("worktree dir missing: %v", err)
	}

	// The worktree is checked out on the nebula's quasar/* branch.
	out, err := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse in worktree: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "quasar/nebula-neb-1" {
		t.Errorf("worktree branch = %q, want quasar/nebula-neb-1", got)
	}

	// Idempotent: a second call reuses the same worktree.
	path2, err := c.EnsureWorktree(ctx, "neb-1")
	if err != nil {
		t.Fatalf("EnsureWorktree (idempotent): %v", err)
	}
	if path2 != path {
		t.Errorf("not idempotent: %q != %q", path2, path)
	}

	// A NEW file commits inside the worktree (git add -A staging + isolation).
	if err := os.WriteFile(filepath.Join(path, "new.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha, err := New(path).Commit(ctx, "add new file", CommitOpts{})
	if err != nil {
		t.Fatalf("commit new file in worktree: %v", err)
	}
	if sha == "" {
		t.Error("expected a commit sha for a new file in the worktree")
	}
	// The operator's main checkout is untouched: still on its original branch.
	out, _ = exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("operator working tree was modified by the build: %s", out)
	}
}
