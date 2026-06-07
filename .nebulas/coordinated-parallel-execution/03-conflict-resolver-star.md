+++
id = "conflict-resolver-star"
title = "Specialist conflict-resolver star with a rich prompt context that preserves both workstreams' intent: both phase specs, both diffs, entanglement state, build errors. Markers mode and no-markers mode."
type = "task"
priority = 1
depends_on = ["entanglement-lifecycle", "merge-gate"]
scope = [
    "internal/artifacts/defaults/stars/conflict-resolver.md",
    "internal/artifacts/defaults/skills/conflict-resolution-rules.md",
    "internal/artifacts/defaults/constellations/merge-conflict-resolve.toml",
    "internal/constellations/operators_conflict.go",
    "internal/constellations/operators_conflict_test.go",
]
+++

## Problem

A conflict isn't a corruption to clean up; it's two valid intents that collided on a shared resource, and the resolution must preserve both. A generic "fix the merge" prompt gives the LLM no way to distinguish "this hunk is the right one to keep" from "both hunks were correct in their own context but the contract between them is what's actually wrong." The resolver almost always picks the wrong side or invents a third side that satisfies neither spec.

The shift in framing — preserve both intents, reconcile the contract — only works if the resolver actually has both intents in its prompt. This phase ships a specialist star whose prompt context includes:

- Both originating phases' full `## Problem` and `## Solution` sections
- Both diffs against the base branch
- The entanglement state across both phases (what each declared, produced, deprecated)
- Any build error output (no-markers mode)
- A skill that codifies the rubric for resolving each case

Two modes — `markers` and `no_markers` — share the same star but get different rubric sections injected.

## Solution

### The star

`internal/artifacts/defaults/stars/conflict-resolver.md`:

```markdown
+++
name = "conflict-resolver"
model = "claude-haiku-4-5-20251001"
fallback_model = "claude-sonnet-4-6"
skills = ["conflict-resolution-rules", "git-aware", "prompt-cache-aware"]

[tools]
allowed = [
    "Read",
    "Edit",
    "Bash(git status *)",
    "Bash(git diff *)",
    "Bash(go build *)",
    "Bash(go vet *)",
    "Bash(go test -short *)",
]
denied = [
    "Bash(git push *)",
    "Bash(git commit *)",
    "Bash(git merge *)",
    "Bash(git reset *)",
    "Bash(git checkout *)",
    "Write",
]

[output]
format = "json"
schema = "conflict-resolution-result-v1"
+++

You are reconciling work from two parallel workstreams. Both produced valid,
intentional changes to the same code path. Your job is NOT to pick a winner.
Your job is to **preserve both intents** while reconciling the contract
between them.

The render_conflict_context operator has already assembled the structured
context below. Read it in order:

1. Workstream A's spec and diff — what A is trying to accomplish
2. Workstream B's spec and diff — what B is trying to accomplish
3. The entanglement state — which symbols each phase declared, produced,
   or deprecated, including current signatures
4. The conflict signal — either the conflicted files with markers, OR
   the post-merge build error output

For each conflicted region (markers mode) or each build error (no_markers
mode), apply the rubric in the `conflict-resolution-rules` skill. Use Edit
to write the resolved content. Verify with `go build ./...` before
returning your final JSON.

Your output MUST match `conflict-resolution-result-v1`:
{
  "status": "resolved" | "needs_human",
  "files_changed": ["path1", "path2"],
  "build_passed": true | false,
  "escalation_reason": null | "string"   // non-null if status=needs_human
}
```

### The skill (rubric)

`internal/artifacts/defaults/skills/conflict-resolution-rules.md`:

```markdown
+++
name = "conflict-resolution-rules"
+++

## Resolving marker conflicts

For each conflict region:

- **Both sides added new entries to a list** (slice append, map entries,
  switch cases): keep both, in source-order from each side.
- **Both sides modified the same line of code**: prefer the version that
  matches the producer's declared signature in the entanglement state.
  If both are consumers of a third symbol, prefer the version that
  compiles.
- **Imports diverged**: take the union, sort, dedupe. Let `goimports`
  normalize.
- **One side deleted a file; the other modified it**: STOP and request
  human review. Do not guess intent here.
- **Config file conflict** (.quasar.yaml, nebula.toml, go.mod, package.json):
  STOP and request human review. Config changes have semantic implications
  beyond the file content.

## Resolving no-marker (semantic) conflicts

Build failure with no markers means the two phases' completed work is
inconsistent at the type/signature level. Common patterns:

- **`undefined: Foo`** after merge — Foo was deprecated by one side and
  used by the other. Check entanglements:
  - If Foo's entanglement is `deprecated` with a replacement noted in the
    producer's spec → migrate the consumer's call sites to the replacement
  - If Foo's entanglement is `deprecated` without a clear replacement →
    request human review
  - If Foo's entanglement is `in_flight` with a different signature →
    update the consumer to the current signature
- **`not enough arguments in call to X`** — signature evolved. Update
  consumer call sites to match the producer's current signature (from
  entanglement state).
- **`type T has no field Y`** — same pattern as undefined; migrate to the
  current type shape from the producer's diff.
- **Multiple build errors that don't have a clear producer/consumer
  relationship**: STOP and request human review.

## Universal rules

- Never introduce new functionality. Your scope is reconciliation only.
- Never delete entire files unless one side has clearly deleted it and
  the other's modification is empty or trivially migratable.
- Never reintroduce a `deprecated` symbol.
- If after one pass the build still fails AND the new errors are not a
  subset of the original errors, STOP — you are making it worse.
- Run `go build ./...` after each batch of edits to verify direction.

## Output discipline

When done, emit ONLY the conflict-resolution-result-v1 JSON. Do not
include prose summaries. The runtime parses your JSON to route the next
node.
```

### The context-building operator

`internal/constellations/operators_conflict.go` adds a builtin `render_conflict_context` that stitches the structured context the star consumes:

```go
// render_conflict_context builds the conflict-resolver's prompt context.
// Inputs: src_run_id, dst_run_id (the two colliding runs), mode
// ("markers" | "no_markers"), files_or_build_output.
// Output: rendered prompt context as a single string the resolver reads
// from its user prompt.
func opRenderConflictContext(ctx Context, rt *Runtime, st *State, args map[string]any) (map[string]any, error)
```

The output's `context` field is a single markdown block like:

```markdown
## Workstream A (run abc123, phase rename-integrations-to-sensors, cycle 3)
### Spec — Problem
[full ## Problem section from the phase's .md file]
### Spec — Solution
[full ## Solution section]
### Diff against base
```diff
[git diff base..A-branch]
```
### Entanglements emitted by A
- Sensor (interface, in_flight, signature="…")
- Poll (func, in_flight, signature="…")
- FromTicket (func, deprecated)

## Workstream B (run def456, phase github-sensor-produces-nebula, cycle 1)
### Spec — Problem
[…]
### Spec — Solution
[…]
### Diff against base
```diff
[git diff base..B-branch]
```
### Entanglements emitted by B
- Scheduler (type, in_flight, signature="…")

## How they collided
- Mode: markers
- Conflicted files: internal/sensors/sensors.go, internal/sensors/registry.go

## What you must do
1. Preserve A's intent for the modified contract
2. Preserve B's intent for the consumer code
3. Migrate B's call sites to A's current signatures where needed
```

(In `no_markers` mode the "How they collided" block carries the build error output instead.)

### The constellation

`internal/artifacts/defaults/constellations/merge-conflict-resolve.toml`:

```toml
name        = "merge-conflict-resolve"
description = "Resolve a merge conflict using the conflict-resolver star and a rich prompt context that preserves both workstreams' intent. Up to [meta].max_cycles attempts, then escalates."

[meta]
max_cycles = 2

[[nodes]]
id   = "render"
type = "builtin"
op   = "render_conflict_context"
inputs = {
    src_run_id        = "${inputs.src_run_id}",
    dst_run_id        = "${inputs.dst_run_id}",
    mode              = "${inputs.mode}",
    files             = "${inputs.files}",
    build_output      = "${inputs.build_output}",
    worktree          = "${inputs.worktree}",
}

[[nodes]]
id     = "resolve"
type   = "star"
star   = "conflict-resolver"
inputs = { context = "${nodes.render.context}", worktree = "${inputs.worktree}" }

[[nodes]]
id   = "decide"
type = "builtin"
op   = "conflict_resolution_decision"
inputs = { output = "${nodes.resolve.result}" }

# A successful resolution: commit the merged state and produce the merge SHA.
[[nodes]]
id     = "commit"
type   = "builtin"
op     = "commit"
inputs = { message = "'resolve merge conflict between '" }
when   = "nodes.decide.status == 'resolved' && nodes.decide.build_passed"

# Build still broken after the resolver's pass; allow one more cycle.
[[nodes]]
id     = "another_cycle"
type   = "goto"
target = "render"
when   = "nodes.decide.status == 'resolved' && !nodes.decide.build_passed && cycle < meta.max_cycles"

[[nodes]]
id     = "give_up"
type   = "builtin"
op     = "fail_run"
inputs = {
    reason = "'conflict-resolver could not produce a green build'",
    detail = "${nodes.decide.escalation_reason}",
}
when   = "(nodes.decide.status == 'needs_human') || (nodes.decide.status == 'resolved' && !nodes.decide.build_passed && cycle >= meta.max_cycles)"

[[edges]]
from = "render"
to   = "resolve"

[[edges]]
from = "resolve"
to   = "decide"

[[edges]]
from = "decide"
to   = "commit"
when = "nodes.decide.status == 'resolved' && nodes.decide.build_passed"

[[edges]]
from = "decide"
to   = "another_cycle"
when = "nodes.decide.status == 'resolved' && !nodes.decide.build_passed && cycle < meta.max_cycles"

[[edges]]
from = "decide"
to   = "give_up"
when = "(nodes.decide.status == 'needs_human') || (nodes.decide.status == 'resolved' && !nodes.decide.build_passed && cycle >= meta.max_cycles)"

[[edges]]
from = "commit"
to   = "_done"

[[edges]]
from = "give_up"
to   = "_awaiting_human"

[outputs]
state      = "${cycle >= meta.max_cycles ? 'awaiting_human' : 'done'}"
merged_sha = "${nodes.commit.sha}"
```

### The decision operator

`conflict_resolution_decision` is a small builtin that parses the resolver's JSON output (schema `conflict-resolution-result-v1`) and rejects malformed responses. Same pattern as `reviewer_decision` and `master_review_decision` from the existing codebase.

### Escalation rules (universal, not per-cycle)

Independent of the cycle cap, certain conditions force immediate `_awaiting_human` without consuming a cycle:

- Any conflicted file path matching the config-file allowlist (`.quasar.yaml`, `nebula.toml`, `go.mod`, `package.json`, `Cargo.toml`, etc.)
- Any `delete-vs-modify` collision on a path under `internal/` or `cmd/`
- The resolver's JSON output is `status: "needs_human"`

These checks happen inside `conflict_resolution_decision`, so the operator returns a verdict the constellation's edges already route on.

### Telemetry

Every conflict-resolver invocation emits a JSONL row to `conflict_resolutions.jsonl`:

```json
{"ts":"...","src_run":"...","dst_run":"...","mode":"markers","cycles":1,"status":"resolved","files_changed":3,"latency_ms":18200,"cost_usd":0.42}
```

A `quasar conflicts report --since 7d` walker surfaces:
- Resolution rate (resolved / total)
- Average cost and latency per resolution
- Top file paths involved in conflicts (signals where the codebase has structural cross-cutting concerns)

### Tests

- `operators_conflict_test.go`: `render_conflict_context` produces a deterministic block for a fixture pair of runs + diffs
- `operators_conflict_test.go`: `conflict_resolution_decision` parses well-formed JSON; rejects malformed; honors config-file escalation
- `merge-conflict-resolve.toml` integration: fixture markers case → resolver runs once → status=resolved+build_passed → commit → _done
- `merge-conflict-resolve.toml` integration: fixture no_markers case where the build still fails after one cycle → another_cycle → max_cycles exceeded → _awaiting_human
- `merge-conflict-resolve.toml` integration: config-file conflict → immediate `_awaiting_human` (no cycles consumed)

## Files

- `internal/artifacts/defaults/stars/conflict-resolver.md` (new)
- `internal/artifacts/defaults/skills/conflict-resolution-rules.md` (new)
- `internal/artifacts/defaults/constellations/merge-conflict-resolve.toml` (new)
- `internal/constellations/operators_conflict.go` (new) — `render_conflict_context` + `conflict_resolution_decision`
- `internal/constellations/operators_conflict_test.go` (new)
- `internal/constellations/operators_conflict_integration_test.go` (new)
- `internal/telemetry/conflict_resolutions.go` (new)
- `internal/telemetry/conflict_resolutions_test.go` (new)
- `cmd/conflicts.go` (new) — `quasar conflicts report --since 7d`

## Acceptance Criteria

- [ ] Conflict-resolver star is registered and loadable; its tools allowlist excludes Write, git push, git commit, git merge, git reset, git checkout
- [ ] `conflict-resolution-rules` skill is loadable and listed by the star
- [ ] `render_conflict_context` produces a deterministic markdown block containing both phases' Problem/Solution sections, both diffs, both phases' entanglements, and either conflicted files OR build output per mode
- [ ] `conflict_resolution_decision` parses `conflict-resolution-result-v1` JSON; rejects malformed input with a field-path error
- [ ] `conflict_resolution_decision` short-circuits to `status=needs_human` when any conflicted file matches the config-file allowlist
- [ ] `conflict_resolution_decision` short-circuits to `status=needs_human` on delete-vs-modify on protected paths
- [ ] The `merge-conflict-resolve` constellation routes resolved+build_passed → commit → _done
- [ ] The constellation routes resolved+!build_passed within cap → another_cycle (re-renders context, re-invokes resolver)
- [ ] The constellation routes needs_human OR resolved+!build_passed beyond cap → give_up → _awaiting_human
- [ ] Per-resolution telemetry row written to `conflict_resolutions.jsonl`
- [ ] `quasar conflicts report --since 7d` prints the resolution-rate / cost / latency summary
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
