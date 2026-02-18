package nebula

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestNewBranchManager(t *testing.T) {
	t.Run("succeeds for valid repo", func(t *testing.T) {
		dir := initTestRepo(t)
		bm, err := NewBranchManager(context.Background(), dir, "my-nebula")
		if err != nil {
			t.Fatalf("NewBranchManager: %v", err)
		}
		if bm.Branch() != "nebula/my-nebula" {
			t.Errorf("Branch() = %q, want %q", bm.Branch(), "nebula/my-nebula")
		}
	})

	t.Run("returns error for non-repo directory", func(t *testing.T) {
		dir := t.TempDir()
		_, err := NewBranchManager(context.Background(), dir, "test")
		if err == nil {
			t.Fatal("expected error for non-repo directory")
		}
	})
}

func TestBranchManager_CreateOrCheckout(t *testing.T) {
	t.Run("creates new branch", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()

		bm, err := NewBranchManager(ctx, dir, "fresh")
		if err != nil {
			t.Fatalf("NewBranchManager: %v", err)
		}

		if err := bm.CreateOrCheckout(ctx); err != nil {
			t.Fatalf("CreateOrCheckout: %v", err)
		}

		// Verify we are on the new branch.
		current := currentBranchHelper(ctx, t, dir)
		if current != "nebula/fresh" {
			t.Errorf("current branch = %q, want %q", current, "nebula/fresh")
		}
	})

	t.Run("checks out existing branch on resume", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()

		// Create the branch manually and switch back to the default branch.
		run(ctx, t, dir, "git", "checkout", "-b", "nebula/resume-test")
		run(ctx, t, dir, "git", "checkout", "-")

		// Verify we are NOT on the nebula branch.
		before := currentBranchHelper(ctx, t, dir)
		if before == "nebula/resume-test" {
			t.Fatal("should not already be on nebula/resume-test")
		}

		bm, err := NewBranchManager(ctx, dir, "resume-test")
		if err != nil {
			t.Fatalf("NewBranchManager: %v", err)
		}

		if err := bm.CreateOrCheckout(ctx); err != nil {
			t.Fatalf("CreateOrCheckout: %v", err)
		}

		// Verify we are now on the existing branch.
		current := currentBranchHelper(ctx, t, dir)
		if current != "nebula/resume-test" {
			t.Errorf("current branch = %q, want %q", current, "nebula/resume-test")
		}
	})
}

func TestBranchManager_EnsureBranch(t *testing.T) {
	t.Run("succeeds when on correct branch", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()

		bm, err := NewBranchManager(ctx, dir, "verify")
		if err != nil {
			t.Fatalf("NewBranchManager: %v", err)
		}
		if err := bm.CreateOrCheckout(ctx); err != nil {
			t.Fatalf("CreateOrCheckout: %v", err)
		}

		if err := bm.EnsureBranch(ctx); err != nil {
			t.Errorf("EnsureBranch should succeed on correct branch: %v", err)
		}
	})

	t.Run("fails when on wrong branch", func(t *testing.T) {
		dir := initTestRepo(t)
		ctx := context.Background()

		bm, err := NewBranchManager(ctx, dir, "wrong-check")
		if err != nil {
			t.Fatalf("NewBranchManager: %v", err)
		}

		// Don't switch branches — we should still be on the default.
		err = bm.EnsureBranch(ctx)
		if err == nil {
			t.Fatal("EnsureBranch should return error when on wrong branch")
		}
		if !strings.Contains(err.Error(), "expected branch") {
			t.Errorf("error should mention expected branch, got: %v", err)
		}
	})
}

func TestBranchManager_NilSafe(t *testing.T) {
	ctx := context.Background()
	var bm *BranchManager

	if bm.Branch() != "" {
		t.Errorf("nil Branch() = %q, want empty", bm.Branch())
	}
	if err := bm.CreateOrCheckout(ctx); err != nil {
		t.Errorf("nil CreateOrCheckout: %v", err)
	}
	if err := bm.EnsureBranch(ctx); err != nil {
		t.Errorf("nil EnsureBranch: %v", err)
	}
}

func TestSlugifyBranch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"already clean", "my-nebula", "my-nebula"},
		{"spaces to hyphens", "TUI Refinement & Visual Polish", "tui-refinement-visual-polish"},
		{"underscores to hyphens", "foo_bar_baz", "foo-bar-baz"},
		{"special chars stripped", "hello~world^2", "helloworld2"},
		{"colons stripped", "fix:bug", "fixbug"},
		{"dots preserved", "v1.2.3", "v1.2.3"},
		{"leading trailing hyphens trimmed", "--edge--case--", "edge-case"},
		{"slashes become hyphens", "a/b/c", "a-b-c"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := slugifyBranch(tc.in)
			if got != tc.want {
				t.Errorf("slugifyBranch(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNewBranchManager_slugifies_name(t *testing.T) {
	dir := initTestRepo(t)
	bm, err := NewBranchManager(context.Background(), dir, "TUI Refinement & Visual Polish")
	if err != nil {
		t.Fatalf("NewBranchManager: %v", err)
	}
	want := "nebula/tui-refinement-visual-polish"
	if bm.Branch() != want {
		t.Errorf("Branch() = %q, want %q", bm.Branch(), want)
	}
}

// currentBranchHelper returns the current branch name in the given repo.
func currentBranchHelper(ctx context.Context, t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse --abbrev-ref HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}
