+++
id = "extract-master-reviewer-star"
title = "Extract master-reviewer prompt + behavior into a star + skill; deprecate the in-loop master-reviewer struct"
type = "task"
priority = 2
scope = [
    "internal/stars/defaults/master-reviewer.md",
    "internal/stars/defaults/skills/master-review-rubric.md",
    "internal/loop/master_reviewer.go",
    "internal/loop/master_reviewer_test.go",
    "internal/runtime/operators/master_review_decision.go",
]
+++

## Problem

`internal/loop/master_reviewer.go` predates constellations. It owns the prompt template, the rubric, the parse-decision logic, and the retry counter — all of which now belong distributed across:

- A **star** (the character: name, model, tools, prompt template) lives under `internal/stars/defaults/master-reviewer.md`
- A **skill** (the rubric: what to check, how to score, the JSON output schema) lives under `internal/stars/defaults/skills/master-review-rubric.md`
- A **builtin operator** (`master_review_decision`) consumes the star's JSON output and returns a typed `{verdict, reasons, suggestions}` for the constellation engine to route on

Once these pieces exist, `internal/loop/master_reviewer.go` becomes a thin adapter that delegates to the runtime when invoked in-process, or is deleted entirely if no consumers remain.

## Solution

### Star definition

`internal/stars/defaults/master-reviewer.md`:

```markdown
+++
name = "master-reviewer"
model = "claude-opus-4-7"
skills = ["git-aware", "master-review-rubric"]

[tools]
allowed = ["Read", "Glob", "Grep", "Bash(git diff *)", "Bash(git log *)", "Bash(go vet *)", "Bash(go test *)"]
denied = ["Edit", "Write", "Bash(git push *)", "Bash(gh pr *)"]

[output]
format = "json"
schema = "master-review-decision-v1"
+++

You are the master reviewer. You receive a nebula and the worktree state after a coder-reviewer cycle. Apply the rubric to decide whether the change is ready to ship.

Your output MUST be a single JSON object matching `master-review-decision-v1`. Do not output prose outside the JSON.
```

The `denied` list enforces read-only — the master reviewer never edits.

### Skill (rubric)

`internal/stars/defaults/skills/master-review-rubric.md`:

```markdown
+++
name = "master-review-rubric"
version = 1
+++

## Output schema: master-review-decision-v1

```json
{
  "verdict": "approve" | "request_changes" | "abandon",
  "score": 0-100,
  "reasons": [{"category": "correctness|tests|safety|style|scope", "detail": "string"}],
  "suggestions": ["string"],   // empty if verdict == "approve"
  "blocker": null | "string"   // non-null if verdict == "abandon"
}
```

## Rubric

- **correctness** — does the change solve the stated problem? Read the nebula's `## Problem` and `## Acceptance Criteria`.
- **tests** — does every changed code path have a test that would fail before the change?
- **safety** — does the change respect the gitops perimeter? Any new `exec.Command("git", ...)` outside `internal/gitops` is a `safety` blocker.
- **style** — does the change follow CLAUDE.md conventions (interfaces consumed-side, error handling explicit, ~20 LOC functions)?
- **scope** — does the change stay within the nebula's `scope = [...]` globs?

Score weighting: correctness 40, tests 30, safety 20, style 5, scope 5.

`verdict = approve` requires score ≥ 80 AND no `safety` reasons.
`verdict = abandon` is reserved for unrecoverable scope/architecture violations.
```

### Operator

`internal/runtime/operators/master_review_decision.go` is a builtin operator (registered alongside the Phase 5 operators):

```go
// MasterReviewDecision parses a master-reviewer star's JSON output into a typed
// decision and returns it to the constellation engine for routing.
//
// Inputs:  star_invocation output (raw JSON)
// Outputs: {verdict, score, reasons, suggestions, blocker}
//
// Errors if the output is not valid JSON or doesn't match schema v1.
// The constellation routes on `verdict` via a `when:` expression:
//   when = "decision.verdict == 'approve'"
func MasterReviewDecision(ctx Context, input json.RawMessage) (Output, error)
```

### Deprecation of internal/loop/master_reviewer.go

Two options, decided in this phase:

**Option A** — delete it entirely. Any in-process consumers move to invoking the runtime via `Runtime.Step()`. Cleaner.

**Option B** — keep a thin shim that wraps the runtime call, marked `Deprecated:` in its godoc. Smaller blast radius.

We go with **Option A**. The `cmd/run.go` command currently uses `loop.Run` which uses master_reviewer; that command becomes a wrapper around `runtime.Fire("master-review", nebula)`. No other consumers exist after grepping.

### Tests

- `internal/runtime/operators/master_review_decision_test.go` — table tests for: valid JSON → typed decision; invalid JSON → error; missing fields → error with field path
- `internal/stars/defaults/master-reviewer.md` is consumed by the file-loader Phase 2 tests; verify it parses with no extra setup
- Delete `internal/loop/master_reviewer_test.go` along with the file

## Files

- `internal/stars/defaults/master-reviewer.md` (new)
- `internal/stars/defaults/skills/master-review-rubric.md` (new)
- `internal/runtime/operators/master_review_decision.go` (new)
- `internal/runtime/operators/master_review_decision_test.go` (new)
- `internal/loop/master_reviewer.go` (delete)
- `internal/loop/master_reviewer_test.go` (delete)
- `cmd/run.go` (modify) — replace `loop.Run` master-review invocation with `runtime.Fire("master-review", nebula)`

## Acceptance Criteria

- [ ] `internal/stars/defaults/master-reviewer.md` exists and parses via the Phase 2 loader
- [ ] `internal/stars/defaults/skills/master-review-rubric.md` exists and parses
- [ ] `operators.MasterReviewDecision` validates input against `master-review-decision-v1` schema; rejects malformed input with a field-path error
- [ ] `internal/loop/master_reviewer.go` and its test are deleted; no remaining import of `loop.MasterReviewer` in the codebase
- [ ] `cmd/run.go` invokes the runtime, not `internal/loop`, for master review
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
