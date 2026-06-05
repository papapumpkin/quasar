+++
id = "coder-reviewer-as-inner-constellation"
title = "Collapse internal/loop into the coder-reviewer constellation; runner package is just a CLI-facing shim"
type = "task"
priority = 3
depends_on = ["extract-master-reviewer-star", "cycle-limit-in-constellation", "budget-propagation"]
scope = [
    "internal/runner/**",
    "internal/loop/**",
    "internal/stars/defaults/constellations/coder-reviewer.toml",
]
+++

## Problem

After the previous three phases, `internal/loop/` (renamed to `internal/runner/` in Phase 1) is mostly empty — its master-reviewer, cycle-limiting, and budget-tracking responsibilities have all moved into the runtime. The inner coder-reviewer pair (a coder produces a diff, a reviewer comments, the coder revises) is the last piece that still lives there.

This phase moves it into the `coder-reviewer` constellation (already declared in Phase 6 of constellation-runtime) as a proper TOML workflow with its own inner cycle counter. After this phase, `internal/runner/` is purely a thin CLI shim that calls `runtime.Fire`.

## Solution

### coder-reviewer constellation (final form)

`internal/stars/defaults/constellations/coder-reviewer.toml`:

```toml
name = "coder-reviewer"
description = "Coder writes a diff; reviewer comments; coder revises. Up to inner_max_cycles loops."

[meta]
inner_max_cycles = 3

[[nodes]]
id = "coder"
kind = "star"
ref  = "coder"
inputs = { nebula = "${nebula}", review = "${reviewer.comments | default('')}" }

[[nodes]]
id = "reviewer"
kind = "star"
ref  = "reviewer"
inputs = { nebula = "${nebula}", diff = "${coder.diff}" }

[[nodes]]
id = "reviewer-decision"
kind = "builtin"
op   = "reviewer_decision"
inputs = { raw = "${reviewer.output}" }

[[nodes]]
id = "revise"
kind = "goto"
target = "coder"
when = "reviewer-decision.verdict == 'request_changes' && cycle < meta.inner_max_cycles"

[[nodes]]
id = "done"
kind = "builtin"
op   = "noop"
when = "reviewer-decision.verdict == 'approve' || cycle >= meta.inner_max_cycles"
outputs = { diff = "${coder.diff}", cycles = "${cycle}" }
```

The `${reviewer.comments | default('')}` syntax provides the empty-on-first-pass behavior the user already saw in the loop code.

### Reviewer decision operator

`internal/runtime/operators/reviewer_decision.go` parses the reviewer star's structured output (`{verdict: approve|request_changes, comments: [...]}`) the same way `master_review_decision` does. It's a separate operator because the schemas differ — the reviewer doesn't score or abandon, it only requests changes or approves.

### internal/runner/ — final form

After this phase `internal/runner/` contains exactly one file:

```go
// Package runner is the CLI-facing shim that translates a `quasar run` invocation
// into a runtime.Fire call. All real work lives in internal/runtime.
package runner

func Run(ctx context.Context, opts Opts) (*Result, error) {
    rt, err := runtime.Open(opts.RuntimeOpts)
    if err != nil { return nil, err }
    return rt.Fire(ctx, "master-review", opts.NebulaID, opts.Overrides)
}

type Opts struct {
    NebulaID    string
    Overrides   runtime.Overrides
    RuntimeOpts runtime.OpenOpts
}

type Result = runtime.RunResult
```

Everything else under the old `internal/loop/` is deleted: the coder/reviewer/master-reviewer structs, the prompt builders (now in star markdown files), the state machine.

### Tests

- Integration test: fire `coder-reviewer` against a fixture nebula where the reviewer requests changes once then approves; assert two coder invocations, then approve
- Cap test: reviewer always requests changes; engine completes 3 cycles then exits with `done` because `cycle >= meta.inner_max_cycles`
- Runner shim test: `runner.Run` produces the same Result struct as direct `runtime.Fire` (golden equality)

### Documentation

Update `CLAUDE.md`'s project structure section to reflect:

```
internal/
  runner/        Thin CLI shim → runtime.Fire (formerly internal/loop)
  runtime/       Constellation engine + operators + budget + cycle guard
  stars/         Embedded default stars/skills/constellations + per-repo override
```

## Files

- `internal/stars/defaults/constellations/coder-reviewer.toml` (modify) — final TOML from this phase replaces the Phase-6 stub
- `internal/runtime/operators/reviewer_decision.go` (new)
- `internal/runtime/operators/reviewer_decision_test.go` (new)
- `internal/runner/runner.go` (rewrite — final thin shim)
- `internal/runner/runner_test.go` (rewrite)
- `internal/loop/` (delete remaining files; directory becomes empty and is removed)
- `CLAUDE.md` (modify) — update project-structure section

## Acceptance Criteria

- [ ] `coder-reviewer.toml` constellation drives the coder-reviewer pair end-to-end with no Go code in `internal/runner/` understanding the loop semantics
- [ ] `reviewer_decision` operator parses reviewer output into typed `{verdict, comments}`
- [ ] Inner cycle cap (`meta.inner_max_cycles`) is enforced by the same `CycleGuard` from Phase 1 with no special-casing
- [ ] `internal/runner/runner.go` is < 50 LOC and contains only the `Fire` shim
- [ ] All previously-existing internal/loop/*.go files are deleted (zero remaining)
- [ ] `CLAUDE.md` project-structure block updated
- [ ] All existing CLI tests for `quasar run` still pass
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
