+++
id = "merge-gate"
title = "Supervisor merge gate after worktree completion: try git merge into the parent branch, classify outcome (clean | markers | build-failure), fire the conflict-resolver constellation on conflict"
type = "task"
priority = 1
depends_on = ["entanglement-lifecycle"]
scope = [
    "internal/gitops/merge_attempt.go",
    "internal/gitops/merge_attempt_test.go",
    "internal/constellations/operators_merge.go",
    "internal/constellations/operators_merge_test.go",
    "internal/artifacts/defaults/constellations/merge-gate.toml",
    "internal/runtime/supervisor.go",
]
+++

## Problem

After Phase 03 of `coder-runtime-hardening` lands and coders run in isolated worktrees, the runtime needs a place to detect cross-phase conflicts before declaring a phase done. The right place is right after the phase's run terminates `done` in its worktree: try the merge, and react to the outcome.

Three outcomes the gate must distinguish:

1. **Clean merge** — `git merge` produces zero conflict markers and `go build ./... && go test ./...` pass. Fulfill the phase.
2. **Marker conflicts** — `git merge` produces `<<<<<<<` markers in one or more files. Fire the conflict-resolver constellation (Phase 03 of this nebula).
3. **Post-merge build failure** — merge has no markers but the build fails. The two phases collided semantically (e.g. one renamed `Foo`, the other added a call to `Foo`). Fire the conflict-resolver constellation in "no markers" mode.

Today none of this exists. The supervisor declares a phase done as soon as its run terminates `done`, then learns about conflicts only when a human notices a broken main branch days later.

## Solution

### The merge attempt primitive

`internal/gitops/merge_attempt.go`:

```go
// MergeAttempt tries to merge srcBranch into dstBranch in a temporary
// worktree and classifies the outcome. The repo's working tree is never
// modified — the attempt happens in a sibling worktree under
// .git/worktrees/quasar/merge-<run-id>/, cleaned up at return.
type MergeAttempt struct {
    Client *Client  // existing gitops.Client
}

type MergeOutcome struct {
    Result        MergeResult        // clean | markers | build_failure | merge_error
    Worktree      string             // path to the merge worktree (for hand-off to resolver)
    ConflictedFiles []string         // files with conflict markers, when Result=markers
    BuildOutput   string             // combined stdout/stderr, when Result=build_failure
    MergedSHA     string             // merge commit SHA, when Result=clean
}

type MergeResult string
const (
    MergeClean         MergeResult = "clean"
    MergeMarkers       MergeResult = "markers"
    MergeBuildFailure  MergeResult = "build_failure"
    MergeError         MergeResult = "merge_error"  // git merge itself failed (e.g. corrupt object)
)

// Try merges srcBranch into dstBranch in a temp worktree, runs a
// configurable verification command (default: go build ./... && go test
// -short ./...), and returns the classified outcome. Always cleans up
// the worktree before return unless KeepWorktree is true (used by the
// conflict resolver to inherit the merge-state worktree).
func (m *MergeAttempt) Try(ctx context.Context, opts TryOpts) (MergeOutcome, error)

type TryOpts struct {
    SrcBranch     string  // e.g. "quasar/cycle-3-of-run-xyz"
    DstBranch     string  // e.g. "main" or "quasar/integration"
    VerifyCommand string  // default: "go build ./... && go test -short ./..."
    KeepWorktree  bool    // if true, the conflict resolver takes ownership
}
```

The worktree path is deterministic per run so the resolver can locate it without an extra argument.

### Operator wiring

A new builtin operator `merge_attempt`:

```go
// Output:
//   {result: "clean" | "markers" | "build_failure" | "merge_error",
//    conflicted_files: ["..."],
//    build_output: "...",
//    merged_sha: "...",
//    worktree_path: "..."}
func opMergeAttempt(ctx Context, rt *Runtime, st *State, args map[string]any) (map[string]any, error)
```

Inputs are pulled from the parent constellation: `src_branch`, `dst_branch`, optional `verify_command`.

### The merge-gate constellation

`internal/artifacts/defaults/constellations/merge-gate.toml`:

```toml
name = "merge-gate"
description = "Attempt a merge from the run's branch into its parent; classify the outcome; route to conflict-resolver on markers or build_failure."

[[nodes]]
id   = "attempt"
type = "builtin"
op   = "merge_attempt"
inputs = { src_branch = "${nebula.src_branch}", dst_branch = "${nebula.dst_branch}" }

# Clean merge → mark fulfilled. The fulfill_entanglements builtin transitions
# every in_flight entanglement for this run to fulfilled, then the supervisor
# commits the merge SHA to the parent branch.
[[nodes]]
id = "fulfill"
type = "builtin"
op   = "fulfill_entanglements"
inputs = { merged_sha = "${nodes.attempt.merged_sha}" }

# Marker conflicts → delegate to the conflict-resolver constellation. The
# resolver inherits the merge worktree (keep_worktree=true via the resolver's
# config), and its [outputs] block reports whether it ultimately succeeded.
[[nodes]]
id = "resolve_markers"
type = "constellation"
ref  = "merge-conflict-resolve"
inputs = {
    worktree = "${nodes.attempt.worktree_path}",
    files    = "${nodes.attempt.conflicted_files}",
    mode     = "'markers'",
}

# Build failure with no markers → same resolver, "no_markers" mode. The
# resolver's prompt branch handles the semantic-conflict case.
[[nodes]]
id = "resolve_build_failure"
type = "constellation"
ref  = "merge-conflict-resolve"
inputs = {
    worktree    = "${nodes.attempt.worktree_path}",
    build_output = "${nodes.attempt.build_output}",
    mode        = "'no_markers'",
}

# After the resolver finishes, mark fulfilled if it succeeded; otherwise
# escalate. The resolver's outputs.state is the run state of its inner run.
[[nodes]]
id = "post_resolve_fulfill"
type = "builtin"
op   = "fulfill_entanglements"
inputs = { merged_sha = "${nodes.resolve_markers.merged_sha | nodes.resolve_build_failure.merged_sha}" }

[[edges]]
from = "attempt"
to   = "fulfill"
when = "nodes.attempt.result == 'clean'"

[[edges]]
from = "attempt"
to   = "resolve_markers"
when = "nodes.attempt.result == 'markers'"

[[edges]]
from = "attempt"
to   = "resolve_build_failure"
when = "nodes.attempt.result == 'build_failure'"

[[edges]]
from = "attempt"
to   = "_failed"
when = "nodes.attempt.result == 'merge_error'"

[[edges]]
from = "fulfill"
to   = "_done"

[[edges]]
from = "resolve_markers"
to   = "post_resolve_fulfill"
when = "nodes.resolve_markers.state == 'done'"

[[edges]]
from = "resolve_markers"
to   = "_awaiting_human"
when = "nodes.resolve_markers.state != 'done'"

[[edges]]
from = "resolve_build_failure"
to   = "post_resolve_fulfill"
when = "nodes.resolve_build_failure.state == 'done'"

[[edges]]
from = "resolve_build_failure"
to   = "_awaiting_human"
when = "nodes.resolve_build_failure.state != 'done'"

[[edges]]
from = "post_resolve_fulfill"
to   = "_done"
```

### Supervisor extension

`internal/runtime/supervisor.go`: after a phase's primary constellation run terminates `done`, the supervisor fires the merge-gate constellation as a follow-on. The phase isn't marked fulfilled in the state file until the merge-gate's own run reaches `_done`.

This means a phase can now have **three** distinct terminal classes:

1. `done` after the coder-reviewer reaches _done AND the merge gate reaches _done → fully fulfilled
2. `awaiting_human` after the merge gate's conflict resolver couldn't resolve → human must intervene
3. `failed` after any pre-merge failure OR a `merge_error` outcome

The TUI Recent lane (from Phase 02 of `master-reviewer-loop-hardening`) already handles the rendering — it just reads the underlying state.

### Verify command per-repo

The verify command defaults to Go-centric (`go build ./... && go test -short ./...`). Per-repo override via `.quasar.yaml`:

```yaml
merge_gate:
  verify_command: "make ci"
  verify_timeout: "5m"
```

A repo without a `merge_gate` block uses the default. Timeout protects against a runaway verify that blocks the supervisor forever.

### What this does NOT do

- Does not attempt three-way reconciliation algorithms beyond standard git merge. The expected use case is: most merges are clean; a small percentage are markers; a smaller percentage are no-marker build failures.
- Does not run the verify command as part of in-cycle commits. That's the coder-reviewer's job. The merge gate only re-runs verify after the merge introduces new content.
- Does not handle file deletions on one side vs modifications on the other automatically — those route to `_awaiting_human` via the resolver's escalation rules (Phase 03).

### Tests

- `merge_attempt_test.go` with fixture repos: clean merge case; marker case; no-marker build-failure case; merge_error case
- `operators_merge_test.go`: the operator returns the right outputs map for each MergeResult
- Integration: a fixture phase whose worktree merges cleanly → merge-gate constellation reaches `_done`; a fixture whose worktree produces markers → merge-gate routes to `resolve_markers`

## Files

- `internal/gitops/merge_attempt.go` (new)
- `internal/gitops/merge_attempt_test.go` (new)
- `internal/constellations/operators_merge.go` (new) — `merge_attempt` + `fulfill_entanglements` builtins
- `internal/constellations/operators_merge_test.go` (new)
- `internal/artifacts/defaults/constellations/merge-gate.toml` (new)
- `internal/runtime/supervisor.go` (modify) — fire merge-gate as the follow-on after coder-reviewer reaches `_done`
- `internal/config/config.go` (modify) — add `MergeGate{VerifyCommand, VerifyTimeout}` block

## Acceptance Criteria

- [ ] `MergeAttempt.Try` performs the merge in `.git/worktrees/quasar/merge-<run-id>/` and never touches the main working tree
- [ ] A clean merge returns `Result=clean` with `MergedSHA` populated
- [ ] A merge with `<<<<<<<` markers returns `Result=markers` with `ConflictedFiles` enumerated
- [ ] A merge with no markers but a non-zero verify exit returns `Result=build_failure` with `BuildOutput` populated
- [ ] A merge that fails at the git level (corrupt object, missing ref) returns `Result=merge_error`
- [ ] Worktree cleanup happens on return UNLESS `KeepWorktree=true`
- [ ] `merge_attempt` operator output schema matches the spec above
- [ ] The `merge-gate.toml` constellation routes each MergeResult to the documented next node
- [ ] Supervisor fires merge-gate after a phase's primary run reaches `_done` and does NOT mark the phase fulfilled until merge-gate reaches `_done`
- [ ] Per-repo `merge_gate.verify_command` override takes effect
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
