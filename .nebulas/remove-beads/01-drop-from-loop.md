+++
id = "drop-from-loop"
title = "Remove beads from the loop layer and delete the internal/beads package"
type = "task"
priority = 1
scope = [
    "internal/beads/**",
    "internal/loop/loop.go",
    "internal/loop/loop_test.go",
    "internal/loop/bead_hook.go",
    "internal/loop/prompts.go",
    "internal/loop/prompts_test.go",
    "internal/loop/prompt_fabric_test.go",
    "internal/loop/lint_test.go",
]
+++

## Problem

The `internal/beads` package wraps an external CLI for task tracking, and `internal/loop` is woven through with calls to it (54 references in `loop.go`, 135 in `loop_test.go`, plus `bead_hook.go` and prompt mentions). For Quasar to function autonomously without an external dependency, this coupling has to be removed. The loop layer is the deepest beads consumer, so it has to be unwound first — every layer above (nebula, cmd) depends on `Loop`, and changing the `Loop` struct's surface will break those layers in predictable, fixable ways for subsequent phases.

## Solution

Delete `internal/beads/` entirely and strip every beads import, type reference, method call, and prompt mention from `internal/loop/`. The `Loop` struct loses its `Beads` field; its constructor signature changes accordingly; methods that wrote bead comments or status updates simply no longer perform that side effect.

### Concrete changes

**Delete entire files:**
- `internal/beads/beads.go`
- `internal/beads/beads_test.go`
- `internal/beads/types.go`
- `internal/beads/` (empty directory removed)
- `internal/loop/bead_hook.go`

**`internal/loop/loop.go`:**
- Remove the import of `"github.com/aaronsalm/quasar/internal/beads"`
- Remove the `Beads beads.Client` field from the `Loop` struct
- Update the `Loop` constructor (likely `NewLoop` or similar) to drop the `beads.Client` parameter
- Find every call site within `loop.go` that uses `l.Beads.*` (Comment, UpdateStatus, etc.) and delete the call. The surrounding logic must remain intact — only the bead side effect is removed.
- If error returns existed solely to surface bead-write failures, simplify the signature accordingly
- Remove any `bead_hook` invocations (the file is being deleted)

**`internal/loop/loop_test.go`:**
- Remove `beads.Client` mock implementations and their usage
- Loop constructor calls in tests drop the beads argument
- Assertions that verified bead-write side effects are removed; the rest of each test remains
- If a test was purely about bead behavior (e.g., `TestLoopWritesBeadComment`), delete that test entirely
- Keep the table-driven test structure and stdlib `testing` style as-is (no t.Parallel additions, no test renaming, no migration to a different style)

**`internal/loop/prompts.go`:**
- Remove any mention of `bd remember`, `bd create`, `bd close`, or "beads" from prompt template strings (the coder and reviewer prompts may instruct agents to use beads CLI; those instructions are removed)
- The prompt text around the removed lines is rewritten to flow naturally — do not leave dangling references like "use the task tracker"

**`internal/loop/prompts_test.go`, `prompt_fabric_test.go`, `lint_test.go`:**
- Update assertions that checked for beads mentions in rendered prompts (remove those assertions; do not invert them)
- If a test was about beads-in-prompts specifically, delete it

### Migration notes

- The `phases.bead_id` column (if it exists in the SQLite schema) is **not** dropped in this phase. The schema change happens in the migration that Nebula 1 introduces. For now, the column can remain unused; nothing reads or writes it after this phase.
- Logging that referenced bead activity (e.g., "wrote bead BD-123") is removed wholesale. Do not replace with new logs unless an existing test depends on the log line — in which case adjust the test instead.

### Verification within this phase

Before marking complete, the agent must verify:
```bash
go build ./internal/loop/...
go vet ./internal/loop/...
go test ./internal/loop/...
grep -rn "beads\|bead_" internal/loop/ internal/beads/ 2>/dev/null
```
The first three commands must exit 0. The grep must return nothing (the `internal/beads/` directory should no longer exist).

Note: `go build ./...` at the repo root will **fail** after this phase because the nebula/cmd layers still reference beads. That is expected and fixed in phases 2 and 3. Do not attempt to fix those breakages here — phase scope is the loop layer only.

## Files

- `internal/beads/beads.go` — delete
- `internal/beads/beads_test.go` — delete
- `internal/beads/types.go` — delete
- `internal/loop/bead_hook.go` — delete
- `internal/loop/loop.go` — remove beads import, Beads field, constructor param, and all l.Beads.* call sites
- `internal/loop/loop_test.go` — remove beads mocks, drop beads constructor arg, delete tests that purely exercised beads behavior, keep all other tests intact
- `internal/loop/prompts.go` — remove `bd` CLI instructions and beads mentions from prompt strings
- `internal/loop/prompts_test.go` — update assertions that referenced beads
- `internal/loop/prompt_fabric_test.go` — update assertions that referenced beads
- `internal/loop/lint_test.go` — update assertions that referenced beads

## Acceptance Criteria

- [ ] `internal/beads/` directory no longer exists
- [ ] `internal/loop/bead_hook.go` no longer exists
- [ ] `grep -rn "beads\|bead_" internal/loop/ internal/beads/ 2>/dev/null` returns no matches
- [ ] `grep -rn "bd remember\|bd create\|bd close" internal/loop/` returns no matches
- [ ] `go build ./internal/loop/...` exits 0
- [ ] `go vet ./internal/loop/...` exits 0
- [ ] `go test ./internal/loop/...` exits 0 with no test failures (some tests may be deleted; remaining tests pass)
- [ ] The `Loop` struct does not have a `Beads` field
- [ ] No imports of `github.com/aaronsalm/quasar/internal/beads` remain anywhere in `internal/loop/`
- [ ] `internal/loop/finding_*.go` files are unmodified (finding tracking is unrelated to beads)
- [ ] Coder/reviewer prompts no longer reference `bd` CLI or beads task tracking
