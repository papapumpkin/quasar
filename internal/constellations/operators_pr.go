package constellations

import (
	"context"
	"fmt"
	"strings"

	"github.com/papapumpkin/quasar/internal/forge"
	"github.com/papapumpkin/quasar/internal/gitops"
)

// Operator names. Defined as constants so the registrar in builtins.go and
// the test assertions reference the same string.
const (
	opGitopsPushName = "gitops_push"
	opGHOpenPRName   = "gh_open_pr"
)

// gitopsPusher is the subset of *gitops.Client the gitops_push operator
// needs. Defined here (where consumed) per project convention so the
// operator is testable without instantiating a real Client.
type gitopsPusher interface {
	CurrentBranch(ctx context.Context) (string, error)
	Push(ctx context.Context, branch string) error
}

// forgeOpener is the subset of forge that the gh_open_pr operator needs.
// *forge.OpenPR is a free function, so this exists to inject a fake in tests.
type forgeOpener interface {
	OpenPR(ctx context.Context, opts forge.PROpts) (forge.PRResult, error)
}

// defaultForgeOpener delegates to the real forge.OpenPR.
type defaultForgeOpener struct{}

func (defaultForgeOpener) OpenPR(ctx context.Context, opts forge.PROpts) (forge.PRResult, error) {
	return forge.OpenPR(ctx, opts)
}

// activeForgeOpener is the injection seam — production uses defaultForgeOpener;
// tests overwrite it via a t.Cleanup-guarded swap. Module-level state is the
// minimum-disruption pattern because the operator function signature is fixed
// by the registry.
var activeForgeOpener forgeOpener = defaultForgeOpener{}

// opGitopsPush pushes a branch through the safety perimeter (gitops.Client)
// so the quasar/* ref allowlist and force-push rejection both apply. Output:
// {"pushed": bool, "branch": "<resolved branch name>"}. The resolved branch
// is the input branch when provided, else the worktree's current branch via
// CurrentBranch — the open-pr constellation's input expression resolves to
// empty when nebula.branch isn't a populated snapshot field, and falling
// back keeps the operator functional in that case.
func opGitopsPush(ctx context.Context, rt *Runtime, _ *State, args map[string]any) (map[string]any, error) {
	branch, _ := args["branch"].(string)
	branch = strings.TrimSpace(branch)

	client, err := pusherForRuntime(rt)
	if err != nil {
		return nil, fmt.Errorf("gitops_push: %w", err)
	}

	if branch == "" {
		current, err := client.CurrentBranch(ctx)
		if err != nil {
			return nil, fmt.Errorf("gitops_push: resolve branch: %w", err)
		}
		branch = strings.TrimSpace(current)
	}
	if branch == "" {
		return nil, fmt.Errorf("gitops_push: no branch provided and worktree has no current branch")
	}
	if err := client.Push(ctx, branch); err != nil {
		return nil, fmt.Errorf("gitops_push: push %q: %w", branch, err)
	}
	return map[string]any{"pushed": true, "branch": branch}, nil
}

// opGHOpenPR opens a PR via the forge package (the only place outside
// internal/sensors/github permitted to exec gh; see internal/arch_test/
// safety_test.go). The push is NOT performed here — the upstream node in
// the open-pr constellation is gitops_push. Output: {"pr_opened": bool,
// "pr_url": <string>, "pr_number": <int>}.
func opGHOpenPR(ctx context.Context, rt *Runtime, st *State, args map[string]any) (map[string]any, error) {
	head, _ := args["head"].(string)
	head = strings.TrimSpace(head)

	if head == "" {
		// Same fallback as gitops_push: read the worktree's branch.
		client, err := pusherForRuntime(rt)
		if err != nil {
			return nil, fmt.Errorf("gh_open_pr: %w", err)
		}
		current, err := client.CurrentBranch(ctx)
		if err != nil {
			return nil, fmt.Errorf("gh_open_pr: resolve head: %w", err)
		}
		head = strings.TrimSpace(current)
	}
	if head == "" {
		return nil, fmt.Errorf("gh_open_pr: no head branch resolved")
	}

	base, _ := args["base"].(string) // empty = forge falls back to repo default
	title, _ := args["title"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		// Synthesize a title from the nebula name so the PR is never anonymous.
		// State.Nebula.Name is populated by Fire via SnapshotNebula.
		title = "Quasar: " + st.Nebula.Name
	}
	body, _ := args["body"].(string)
	if strings.TrimSpace(body) == "" {
		body = synthesizePRBody(st)
	}

	result, err := activeForgeOpener.OpenPR(ctx, forge.PROpts{
		WorkDir: rt.repoPath,
		Head:    head,
		Base:    strings.TrimSpace(base),
		Title:   title,
		Body:    body,
	})
	if err != nil {
		return nil, fmt.Errorf("gh_open_pr: %w", err)
	}
	return map[string]any{
		"pr_opened": true,
		"pr_url":    result.URL,
		"pr_number": result.Number,
	}, nil
}

// pusherForRuntime returns the runtime's committer typed as the pusher
// interface. The runtime requires the Committer to be a *gitops.Client
// (the only production implementer); a missing committer is a
// configuration error caught at construction, but we re-check here so the
// operator's failure mode is clear.
func pusherForRuntime(rt *Runtime) (gitopsPusher, error) {
	if rt == nil || rt.committer == nil {
		return nil, fmt.Errorf("runtime has no committer configured")
	}
	client, ok := rt.committer.(*gitops.Client)
	if !ok {
		return nil, fmt.Errorf("runtime committer is %T, not *gitops.Client", rt.committer)
	}
	return client, nil
}

// synthesizePRBody builds a minimal PR description from the nebula's name
// and phase titles when the constellation didn't supply one. Keeps the PR
// from being anonymous in the happy path; richer authoring is a separate
// concern.
func synthesizePRBody(st *State) string {
	if st == nil {
		return "Generated by Quasar."
	}
	var b strings.Builder
	b.WriteString("Generated by Quasar from nebula `")
	b.WriteString(st.Nebula.Name)
	b.WriteString("`.\n")
	if len(st.Nebula.Phases) == 0 {
		return b.String()
	}
	b.WriteString("\n## Phases\n")
	for _, p := range st.Nebula.Phases {
		fmt.Fprintf(&b, "- %s (%s)\n", p.Title, p.ID)
	}
	return b.String()
}
