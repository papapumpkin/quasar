+++
id = "cycle-limit-in-constellation"
title = "Move the 3-cycle master-review cap from loop code into the master-review constellation as a declarative attribute"
type = "task"
priority = 2
depends_on = ["extract-master-reviewer-star"]
scope = [
    "internal/stars/defaults/constellations/master-review.toml",
    "internal/runtime/cycle_guard.go",
    "internal/runtime/cycle_guard_test.go",
    "internal/loop/loop.go",
]
+++

## Problem

The "max 3 master-review cycles before a PR is opened" rule the user specified is currently a hardcoded constant in `internal/loop`. With the master reviewer extracted to the runtime (previous phase), the cycle limit moves with it — but as a *declarative* attribute on the constellation, not another magic number.

This makes the limit:
- Visible in the TOML so operators can tune it without recompiling
- Per-repo overridable (the embedded default of 3 can be overridden by a repo's `constellations/master-review.toml`)
- Per-run overridable via `[execution].max_review_cycles` in nebula manifests (already supported)

## Solution

### Constellation definition

`internal/stars/defaults/constellations/master-review.toml`:

```toml
name = "master-review"
description = "Run the coder-reviewer inner loop, then have the master reviewer judge. Loop up to max_cycles times."

[meta]
max_cycles = 3              # NEW: declarative cap

[[nodes]]
id = "coder-reviewer"
kind = "constellation"
ref  = "coder-reviewer"     # inner constellation from Phase 6

[[nodes]]
id = "master-review"
kind = "star"
ref  = "master-reviewer"
inputs = { nebula = "${nebula}", worktree_diff = "${coder-reviewer.diff}" }

[[nodes]]
id = "decide"
kind = "builtin"
op   = "master_review_decision"
inputs = { raw = "${master-review.output}" }

[[nodes]]
id = "open-pr"
kind = "constellation"
ref  = "open-pr"
when = "decide.verdict == 'approve'"

[[nodes]]
id = "another-cycle"
kind = "goto"
target = "coder-reviewer"
when = "decide.verdict == 'request_changes' && cycle < meta.max_cycles"

[[nodes]]
id = "give-up"
kind = "builtin"
op   = "fail_run"
inputs = {
    reason = "max master-review cycles exhausted",
    detail = "${decide.reasons}",
}
when = "decide.verdict == 'request_changes' && cycle >= meta.max_cycles"

[[nodes]]
id = "abandon"
kind = "builtin"
op   = "fail_run"
inputs = { reason = "master reviewer abandoned", detail = "${decide.blocker}" }
when = "decide.verdict == 'abandon'"
```

The `cycle` identifier is provided by the runtime — every `goto` node back-edge increments it.

### Runtime cycle guard

`internal/runtime/cycle_guard.go` is the bookkeeping. It tracks `cycle` per `constellation_run.id`, validates against `meta.max_cycles`, and short-circuits the `goto` if exceeded (the engine routes to `give-up` instead):

```go
// CycleGuard tracks back-edge traversals per run. A goto node whose target
// has already been visited `meta.max_cycles` times in this run is rejected
// at engine-step time; the engine then evaluates the next matching `when`,
// which is the give-up fallback.
type CycleGuard struct {
    store *fabric.ConstellationRunStore
}

func (g *CycleGuard) RecordEntry(ctx context.Context, runID, nodeID string) (cycle int, err error)
func (g *CycleGuard) Check(ctx context.Context, runID, nodeID string, max int) (allowed bool, err error)
```

Cycle counts are persisted in a new column on `constellation_runs`:

```sql
ALTER TABLE constellation_runs ADD COLUMN cycle_counts BLOB; -- JSON: {"coder-reviewer": 2}
```

JSON blob keeps the schema simple — we never query it from SQL.

### Loop simplification

`internal/loop/loop.go` had its own cycle loop with `maxCycles` from config. That loop is removed; `loop.Run` is replaced by:

```go
func Run(ctx context.Context, opts Opts) (*Result, error) {
    return runtime.Fire(ctx, "master-review", opts.NebulaID, opts.Overrides)
}
```

This is the only thing `internal/loop` does. We rename it `internal/runner` and drop the multi-file structure since everything else (master reviewer, coder, reviewer) has moved out.

### Override semantics

Three layers, last wins:
1. Embedded default: `meta.max_cycles = 3`
2. Repo override at `<repo>/constellations/master-review.toml` (Phase 2 file loader)
3. Per-run override at `nebula.toml` `[execution].max_review_cycles = N`

The runtime resolves the final value at Fire time and stores it in the `constellation_runs` row.

### Tests

- `internal/runtime/cycle_guard_test.go` — table tests: entry increments cycle; check rejects after N; reset between runs; JSON round-trip
- Constellation integration test: with `max_cycles=2`, after 2 master-review iterations of `request_changes`, the run ends in `failed` with reason "max master-review cycles exhausted"
- Override test: per-run override of 5 overrides embedded 3

## Files

- `internal/stars/defaults/constellations/master-review.toml` (new; replaces hardcoded loop)
- `internal/runtime/cycle_guard.go` (new)
- `internal/runtime/cycle_guard_test.go` (new)
- `internal/fabric/migrations/NNN_cycle_counts.sql` (new)
- `internal/loop/loop.go` → `internal/runner/runner.go` (rename + simplify)
- `internal/loop/loop_test.go` → keep covering tests, point at runtime
- `internal/runtime/engine.go` — wire in cycle_guard for `goto` nodes

## Acceptance Criteria

- [ ] `master-review.toml` constellation defines `[meta] max_cycles = 3`
- [ ] Runtime increments `cycle` per back-edge traversal and persists `cycle_counts` JSON on `constellation_runs`
- [ ] After `max_cycles` exhausted, the engine evaluates the `give-up` fallback `when` and the run ends `state='failed'` with structured reason
- [ ] Per-run override (`[execution].max_review_cycles`) replaces the embedded default at Fire time
- [ ] `internal/loop/` is renamed to `internal/runner/` and reduced to a Fire shim
- [ ] No hardcoded cycle constants remain in Go code; arch-test 8-equivalent grep `maxCycles\s*=\s*\d+` returns nothing under `internal/`
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
