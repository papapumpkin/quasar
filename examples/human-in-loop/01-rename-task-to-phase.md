+++
id = "rename-task-to-phase"
title = "Rename task to phase throughout the nebula subsystem"
type = "feature"
priority = 1
depends_on = []
+++

## Problem

The nebula subsystem uses "task" as the name for its work units (`TaskSpec`, `TaskState`, `TaskRunner`, `TaskStatus`, etc.), but "task" is overloaded — beads also has tasks, and every project management tool uses the same word. This creates confusion when discussing nebula work units versus beads issues.

## Solution

Rename all nebula-internal references from "task" to "phase" across types, functions, variables, file constants, TOML keys, and tests. "Phase" rhymes with "quasar," sounds astral, and clearly distinguishes nebula work units from beads tasks.

### Renames

**Types (internal/nebula/types.go):**
- `TaskSpec` → `PhaseSpec`
- `TaskState` → `PhaseState`
- `TaskStatus` → `PhaseStatus`
- `TaskStatusPending` → `PhaseStatusPending` (and all other status constants)
- `WorkerResult.TaskID` → `WorkerResult.PhaseID`
- `Action.TaskID` → `Action.PhaseID`
- `State.Tasks` → `State.Phases`

**Worker (internal/nebula/worker.go):**
- `TaskRunner` → `PhaseRunner`
- `TaskRunnerResult` → `PhaseRunnerResult`
- `WorkerGroup` field/method references to "task" → "phase"
- `buildTaskPrompt` → `buildPhasePrompt`
- `filterEligible` and `hasFailedDep` parameter names

**TOML keys (nebula.state.toml):**
- `[tasks]` section → `[phases]`
- All internal TOML struct tags referencing "task"

**Tests (internal/nebula/*_test.go):**
- Update all test references to use new names

**Parse (internal/nebula/parse.go):**
- Update parsing logic to read phase specs

### Backward Compatibility

During transition, the TOML parser should accept both `[tasks]` and `[phases]` in state files, preferring `[phases]`. Emit a deprecation warning via `ui.Printer` when `[tasks]` is encountered. This can be removed in a future version.

## Files to Modify

- `internal/nebula/types.go` — All type and constant renames
- `internal/nebula/worker.go` — Interface and function renames
- `internal/nebula/state.go` — State serialization keys
- `internal/nebula/parse.go` — Parsing references
- `internal/nebula/apply.go` — Apply logic references
- `internal/nebula/plan.go` — Plan references
- `internal/nebula/graph.go` — Graph references
- `internal/nebula/validate.go` — Validation references
- `internal/nebula/errors.go` — Error message text
- `internal/nebula/config.go` — Config references
- `internal/nebula/nebula_test.go` — Test updates
- `internal/nebula/config_test.go` — Test updates
- `internal/nebula/watcher.go` — NebulaChange.TaskID → PhaseID
- `internal/nebula/watcher_test.go` — Test updates

## Acceptance Criteria

- [ ] No symbol in `internal/nebula/` contains "Task" or "task" (except backward-compat TOML parsing)
- [ ] All tests pass: `go test ./internal/nebula/...`
- [ ] `go vet ./...` passes
- [ ] State files serialize with `[phases]` section
- [ ] Existing `[tasks]` state files still load (with deprecation warning)
- [ ] Example nebula specs updated to reflect new terminology