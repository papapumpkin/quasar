package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitFixture is a throwaway git repo used to exercise MergeAttempt against real
// git rather than a fake, since the merge-classification logic lives in git's
// own conflict detection.
type gitFixture struct {
	t   *testing.T
	dir string
}

// newGitFixture initializes a repo with a configured identity and a single
// committed file on the main branch.
func newGitFixture(t *testing.T) *gitFixture {
	t.Helper()
	dir := t.TempDir()
	f := &gitFixture{t: t, dir: dir}
	f.git("init", "-b", "main")
	f.git("config", "user.email", "quasar@test.local")
	f.git("config", "user.name", "Quasar Test")
	f.write("shared.txt", "base\n")
	f.git("add", "shared.txt")
	f.git("commit", "-m", "base")
	return f
}

// git runs a git subcommand in the fixture, failing the test on error.
func (f *gitFixture) git(args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", f.dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// write creates or overwrites a file in the fixture working tree.
func (f *gitFixture) write(name, content string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o644); err != nil {
		f.t.Fatalf("write %s: %v", name, err)
	}
}

// branchFrom creates branch off the current HEAD, checks it out, applies fn to
// stage and commit a change, then returns to main.
func (f *gitFixture) branchFrom(branch string, fn func()) {
	f.t.Helper()
	f.git("checkout", "-b", branch)
	fn()
	f.git("checkout", "main")
}

func TestMergeAttemptTry(t *testing.T) {
	t.Run("clean merge returns clean with merged sha", func(t *testing.T) {
		f := newGitFixture(t)
		f.branchFrom("quasar/feature", func() {
			f.write("added.go", "package main\n")
			f.git("add", "added.go")
			f.git("commit", "-m", "add file")
		})

		ma := &MergeAttempt{Client: New(f.dir)}
		out, err := ma.Try(context.Background(), TryOpts{
			SrcBranch:     "quasar/feature",
			DstBranch:     "main",
			VerifyCommand: "true",
		})
		if err != nil {
			t.Fatalf("Try: %v", err)
		}
		if out.Result != MergeClean {
			t.Fatalf("Result = %q, want clean (build output: %s)", out.Result, out.BuildOutput)
		}
		if out.MergedSHA == "" {
			t.Error("MergedSHA empty on clean merge")
		}
	})

	t.Run("conflicting edits return markers with conflicted files", func(t *testing.T) {
		f := newGitFixture(t)
		f.branchFrom("quasar/feature", func() {
			f.write("shared.txt", "feature change\n")
			f.git("add", "shared.txt")
			f.git("commit", "-m", "feature edit")
		})
		// Diverge main on the same line so the merge conflicts.
		f.write("shared.txt", "main change\n")
		f.git("add", "shared.txt")
		f.git("commit", "-m", "main edit")

		ma := &MergeAttempt{Client: New(f.dir)}
		out, err := ma.Try(context.Background(), TryOpts{
			SrcBranch:     "quasar/feature",
			DstBranch:     "main",
			VerifyCommand: "true",
		})
		if err != nil {
			t.Fatalf("Try: %v", err)
		}
		if out.Result != MergeMarkers {
			t.Fatalf("Result = %q, want markers", out.Result)
		}
		if len(out.ConflictedFiles) != 1 || out.ConflictedFiles[0] != "shared.txt" {
			t.Errorf("ConflictedFiles = %v, want [shared.txt]", out.ConflictedFiles)
		}
	})

	t.Run("clean merge with failing verify returns build_failure", func(t *testing.T) {
		f := newGitFixture(t)
		f.branchFrom("quasar/feature", func() {
			f.write("added.go", "package main\n")
			f.git("add", "added.go")
			f.git("commit", "-m", "add file")
		})

		ma := &MergeAttempt{Client: New(f.dir)}
		out, err := ma.Try(context.Background(), TryOpts{
			SrcBranch:     "quasar/feature",
			DstBranch:     "main",
			VerifyCommand: "echo boom && exit 1",
		})
		if err != nil {
			t.Fatalf("Try: %v", err)
		}
		if out.Result != MergeBuildFailure {
			t.Fatalf("Result = %q, want build_failure", out.Result)
		}
		if !strings.Contains(out.BuildOutput, "boom") {
			t.Errorf("BuildOutput = %q, want it to contain verify output", out.BuildOutput)
		}
		if out.MergedSHA == "" {
			t.Error("MergedSHA empty: a build_failure still produced a merge commit")
		}
	})

	t.Run("missing source ref returns merge_error", func(t *testing.T) {
		f := newGitFixture(t)
		ma := &MergeAttempt{Client: New(f.dir)}
		out, err := ma.Try(context.Background(), TryOpts{
			SrcBranch:     "quasar/does-not-exist",
			DstBranch:     "main",
			VerifyCommand: "true",
		})
		if err != nil {
			t.Fatalf("Try: %v", err)
		}
		if out.Result != MergeError {
			t.Fatalf("Result = %q, want merge_error", out.Result)
		}
	})

	t.Run("worktree is cleaned up unless kept", func(t *testing.T) {
		f := newGitFixture(t)
		f.branchFrom("quasar/feature", func() {
			f.write("added.go", "package main\n")
			f.git("add", "added.go")
			f.git("commit", "-m", "add file")
		})
		ma := &MergeAttempt{Client: New(f.dir)}

		out, err := ma.Try(context.Background(), TryOpts{
			SrcBranch: "quasar/feature", DstBranch: "main",
			VerifyCommand: "true", RunID: "run-cleanup",
		})
		if err != nil {
			t.Fatalf("Try (cleanup): %v", err)
		}
		if _, statErr := os.Stat(out.Worktree); !os.IsNotExist(statErr) {
			t.Errorf("worktree %s still exists after default cleanup", out.Worktree)
		}

		kept, err := ma.Try(context.Background(), TryOpts{
			SrcBranch: "quasar/feature", DstBranch: "main",
			VerifyCommand: "true", RunID: "run-keep", KeepWorktree: true,
		})
		if err != nil {
			t.Fatalf("Try (keep): %v", err)
		}
		if _, statErr := os.Stat(kept.Worktree); statErr != nil {
			t.Errorf("worktree %s missing despite KeepWorktree: %v", kept.Worktree, statErr)
		}
	})

	t.Run("main working tree is untouched", func(t *testing.T) {
		f := newGitFixture(t)
		f.branchFrom("quasar/feature", func() {
			f.write("added.go", "package main\n")
			f.git("add", "added.go")
			f.git("commit", "-m", "add file")
		})
		mainBefore := f.git("rev-parse", "main")

		ma := &MergeAttempt{Client: New(f.dir)}
		if _, err := ma.Try(context.Background(), TryOpts{
			SrcBranch: "quasar/feature", DstBranch: "main", VerifyCommand: "true",
		}); err != nil {
			t.Fatalf("Try: %v", err)
		}

		if after := f.git("rev-parse", "main"); after != mainBefore {
			t.Errorf("main moved from %s to %s; merge attempt must not advance dst", mainBefore, after)
		}
		if status := f.git("status", "--porcelain"); status != "" {
			t.Errorf("main working tree dirty after merge attempt:\n%s", status)
		}
	})
}
