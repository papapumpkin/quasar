package constellations

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/forge"
)

// fakeOpener captures the PROpts and produces either a canned URL or an error.
type fakeOpener struct {
	gotOpts   forge.PROpts
	result    forge.PRResult
	err       error
	openCalls int

	// FindOpenPR behavior. Defaults (found=false, err=nil) make the operator
	// fall through to OpenPR, matching pre-idempotency-guard behavior.
	findResult forge.PRResult
	findFound  bool
	findErr    error
	findHead   string
	findCalls  int
}

func (f *fakeOpener) OpenPR(_ context.Context, opts forge.PROpts) (forge.PRResult, error) {
	f.openCalls++
	f.gotOpts = opts
	return f.result, f.err
}

func (f *fakeOpener) FindOpenPR(_ context.Context, _, head string) (forge.PRResult, bool, error) {
	f.findCalls++
	f.findHead = head
	return f.findResult, f.findFound, f.findErr
}

// swapForgeOpener replaces the module-level opener for the lifetime of t.
// The opener is module-level state, so tests that touch it must NOT call
// t.Parallel — they would race with each other through the shared variable.
// Cleanup restores the original on test exit.
func swapForgeOpener(t *testing.T, o forgeOpener) {
	t.Helper()
	prev := activeForgeOpener
	activeForgeOpener = o
	t.Cleanup(func() { activeForgeOpener = prev })
}

// stateWithNebula builds a minimal State whose Nebula carries the given
// name and phase list. Operator tests use this to verify the PR-body
// synthesizer reads from State.Nebula.
func stateWithNebula(name string, phases ...PhaseSnapshot) *State {
	st := NewState(NebulaSnapshot{Name: name, Phases: phases}, 0)
	return st
}

func TestOpGHOpenPR_Success(t *testing.T) {
	// No t.Parallel: tests in this file mutate the module-level
	// activeForgeOpener (see swapForgeOpener).
	op := &fakeOpener{result: forge.PRResult{URL: "https://github.com/o/r/pull/7", Number: 7}}
	swapForgeOpener(t, op)

	rt := &Runtime{repoPath: "/tmp/repo"}
	st := stateWithNebula("ship-it")
	out, err := opGHOpenPR(context.Background(), rt, st, map[string]any{
		"head":  "quasar/feature",
		"base":  "main",
		"title": "Quasar: ship-it",
		"body":  "from test",
	})
	if err != nil {
		t.Fatalf("opGHOpenPR: %v", err)
	}
	if out["pr_opened"] != true {
		t.Errorf("pr_opened = %v, want true", out["pr_opened"])
	}
	if got, _ := out["pr_url"].(string); got != "https://github.com/o/r/pull/7" {
		t.Errorf("pr_url = %q, want the canned URL", got)
	}
	if got, _ := out["pr_number"].(int); got != 7 {
		t.Errorf("pr_number = %v, want 7", out["pr_number"])
	}
	if op.gotOpts.Head != "quasar/feature" || op.gotOpts.Base != "main" || op.gotOpts.Title != "Quasar: ship-it" {
		t.Errorf("PROpts forwarded incorrectly: %+v", op.gotOpts)
	}
	if op.gotOpts.WorkDir != "/tmp/repo" {
		t.Errorf("WorkDir = %q, want the runtime's repoPath", op.gotOpts.WorkDir)
	}
}

func TestOpGHOpenPR_IdempotentWhenPRExists(t *testing.T) {
	// No t.Parallel: shared activeForgeOpener. When an open PR already exists
	// for the head branch (e.g. a re-step after a crash), the operator must
	// return it WITHOUT calling OpenPR — gh pr create would otherwise error and
	// fail the whole run.
	op := &fakeOpener{
		findFound:  true,
		findResult: forge.PRResult{URL: "https://github.com/o/r/pull/9", Number: 9},
		// If OpenPR were (wrongly) called it would return a different PR.
		result: forge.PRResult{URL: "https://github.com/o/r/pull/999", Number: 999},
	}
	swapForgeOpener(t, op)

	rt := &Runtime{repoPath: "/tmp/repo"}
	out, err := opGHOpenPR(context.Background(), rt, stateWithNebula("ship-it"), map[string]any{
		"head": "quasar/feature", "title": "t",
	})
	if err != nil {
		t.Fatalf("opGHOpenPR: %v", err)
	}
	if op.openCalls != 0 {
		t.Errorf("OpenPR called %d times; existing PR should short-circuit", op.openCalls)
	}
	if got, _ := out["pr_number"].(int); got != 9 {
		t.Errorf("pr_number = %v, want existing PR #9", out["pr_number"])
	}
	if got, _ := out["pr_url"].(string); got != "https://github.com/o/r/pull/9" {
		t.Errorf("pr_url = %q, want existing PR url", got)
	}
	if op.findHead != "quasar/feature" {
		t.Errorf("FindOpenPR head = %q, want quasar/feature", op.findHead)
	}
}

func TestOpGHOpenPR_FindErrorFallsThroughToCreate(t *testing.T) {
	// No t.Parallel: shared activeForgeOpener. A failed existence lookup is
	// best-effort: it must NOT block opening a PR.
	op := &fakeOpener{
		findErr: errors.New("gh list boom"),
		result:  forge.PRResult{URL: "https://github.com/o/r/pull/3", Number: 3},
	}
	swapForgeOpener(t, op)

	rt := &Runtime{repoPath: "/tmp/repo"}
	out, err := opGHOpenPR(context.Background(), rt, stateWithNebula("ship-it"), map[string]any{
		"head": "quasar/feature", "title": "t",
	})
	if err != nil {
		t.Fatalf("opGHOpenPR: %v", err)
	}
	if op.openCalls != 1 {
		t.Errorf("OpenPR called %d times; a find error must fall through to create", op.openCalls)
	}
	if got, _ := out["pr_number"].(int); got != 3 {
		t.Errorf("pr_number = %v, want created PR #3", out["pr_number"])
	}
}

func TestOpGHOpenPR_SynthesizesTitleWhenMissing(t *testing.T) {
	// No t.Parallel: shared activeForgeOpener.
	// An empty title input must NOT pass through to gh (gh rejects empty
	// titles). The operator synthesizes "Quasar: <nebula name>".
	op := &fakeOpener{result: forge.PRResult{URL: "https://x/pull/1", Number: 1}}
	swapForgeOpener(t, op)

	rt := &Runtime{repoPath: "/tmp/repo"}
	st := stateWithNebula("doc-refresh")
	_, err := opGHOpenPR(context.Background(), rt, st, map[string]any{
		"head":  "quasar/x",
		"title": "",
	})
	if err != nil {
		t.Fatalf("opGHOpenPR: %v", err)
	}
	if want := "Quasar: doc-refresh"; op.gotOpts.Title != want {
		t.Errorf("synthesized title = %q, want %q", op.gotOpts.Title, want)
	}
}

func TestOpGHOpenPR_SynthesizesBodyFromPhases(t *testing.T) {
	// No t.Parallel: shared activeForgeOpener.
	op := &fakeOpener{result: forge.PRResult{URL: "https://x/pull/2", Number: 2}}
	swapForgeOpener(t, op)

	rt := &Runtime{repoPath: "/tmp/repo"}
	st := stateWithNebula("multi-phase",
		PhaseSnapshot{ID: "p1", Title: "Wire the thing"},
		PhaseSnapshot{ID: "p2", Title: "Test the thing"},
	)
	_, err := opGHOpenPR(context.Background(), rt, st, map[string]any{
		"head":  "quasar/x",
		"title": "x",
		"body":  "", // explicit empty → synthesize
	})
	if err != nil {
		t.Fatalf("opGHOpenPR: %v", err)
	}
	if !strings.Contains(op.gotOpts.Body, "multi-phase") {
		t.Errorf("body missing nebula name: %q", op.gotOpts.Body)
	}
	if !strings.Contains(op.gotOpts.Body, "Wire the thing") {
		t.Errorf("body missing phase title: %q", op.gotOpts.Body)
	}
}

func TestOpGHOpenPR_PropagatesForgeError(t *testing.T) {
	// No t.Parallel: shared activeForgeOpener.
	op := &fakeOpener{err: errors.New("gh: bad credentials")}
	swapForgeOpener(t, op)

	rt := &Runtime{repoPath: "/tmp/repo"}
	st := stateWithNebula("x")
	_, err := opGHOpenPR(context.Background(), rt, st, map[string]any{
		"head":  "quasar/x",
		"title": "x",
	})
	if err == nil {
		t.Fatal("expected error from forge failure, got nil")
	}
	if !strings.Contains(err.Error(), "bad credentials") {
		t.Errorf("error %q does not surface the forge cause", err.Error())
	}
}

func TestOpGHOpenPR_RejectsRuntimeWithoutCommitter(t *testing.T) {
	// No t.Parallel: this test doesn't swap the opener but reaches the
	// runtime-check before any opener call, so it's safe to leave serial.
	// Without a committer, the head-fallback (CurrentBranch on the gitops
	// Client) cannot resolve and the operator must refuse rather than
	// silently send an empty branch to gh.
	rt := &Runtime{}
	st := stateWithNebula("x")
	_, err := opGHOpenPR(context.Background(), rt, st, map[string]any{
		"head":  "",
		"title": "x",
	})
	if err == nil {
		t.Fatal("expected error for missing committer, got nil")
	}
}

// stubPusher records the branch the operator chose to push.
type stubPusher struct {
	currentBranch string
	pushed        string
	currentErr    error
	pushErr       error
}

func (s *stubPusher) CurrentBranch(_ context.Context) (string, error) {
	return s.currentBranch, s.currentErr
}

func (s *stubPusher) Push(_ context.Context, branch string) error {
	s.pushed = branch
	return s.pushErr
}

// opGitopsPushWithPusher mirrors opGitopsPush but accepts an explicit pusher
// so the operator's branching logic can be unit-tested without a Runtime.
// The real opGitopsPush extracts the pusher from rt.committer via
// pusherForRuntime; this test variant skips that indirection.
func opGitopsPushWithPusher(ctx context.Context, p gitopsPusher, args map[string]any) (map[string]any, error) {
	branch, _ := args["branch"].(string)
	branch = strings.TrimSpace(branch)
	if branch == "" {
		current, err := p.CurrentBranch(ctx)
		if err != nil {
			return nil, err
		}
		branch = strings.TrimSpace(current)
	}
	if branch == "" {
		return nil, errors.New("no branch")
	}
	if err := p.Push(ctx, branch); err != nil {
		return nil, err
	}
	return map[string]any{"pushed": true, "branch": branch}, nil
}

func TestOpGitopsPush_UsesExplicitBranchWhenProvided(t *testing.T) {
	t.Parallel()
	p := &stubPusher{currentBranch: "main"}
	out, err := opGitopsPushWithPusher(context.Background(), p, map[string]any{
		"branch": "quasar/explicit",
	})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if p.pushed != "quasar/explicit" {
		t.Errorf("pushed = %q, want the explicit input", p.pushed)
	}
	if out["branch"] != "quasar/explicit" {
		t.Errorf("out.branch = %v", out["branch"])
	}
}

func TestOpGitopsPush_FallsBackToCurrentBranch(t *testing.T) {
	t.Parallel()
	// Empty branch input → operator queries the worktree. This is the
	// production path because the open-pr.toml expression references
	// nebula.branch which is not currently a populated NebulaSnapshot
	// field.
	p := &stubPusher{currentBranch: "quasar/auto"}
	_, err := opGitopsPushWithPusher(context.Background(), p, map[string]any{})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if p.pushed != "quasar/auto" {
		t.Errorf("pushed = %q, want the fallback CurrentBranch", p.pushed)
	}
}

func TestOpGitopsPush_RejectsEmptyAfterFallback(t *testing.T) {
	t.Parallel()
	// CurrentBranch returns empty (detached HEAD, fresh repo, etc.) → the
	// operator must refuse rather than push an empty refspec.
	p := &stubPusher{currentBranch: ""}
	_, err := opGitopsPushWithPusher(context.Background(), p, map[string]any{})
	if err == nil {
		t.Fatal("expected error when no branch resolves, got nil")
	}
}

func TestOpGitopsPush_PropagatesPushError(t *testing.T) {
	t.Parallel()
	p := &stubPusher{currentBranch: "quasar/x", pushErr: errors.New("rejected: non-fast-forward")}
	_, err := opGitopsPushWithPusher(context.Background(), p, map[string]any{})
	if err == nil {
		t.Fatal("expected push error, got nil")
	}
}
