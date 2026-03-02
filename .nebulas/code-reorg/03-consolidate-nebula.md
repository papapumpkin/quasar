+++
id = "consolidate-nebula"
title = "Consolidate micro-files and rename config.go in internal/nebula/"
type = "task"
priority = 2
depends_on = ["delete-dead-code"]
scope = ["internal/nebula/"]
+++

## Problem

`internal/nebula/` has 44 non-test source files. Six of them are tiny (2-3 KB each) and belong in their natural parent file. Additionally, `config.go` is confusingly named since `internal/config/config.go` already exists.

Files to consolidate:
1. **`state.go`** (3 KB) — `LoadState()`, `SaveState()`, `SetPhaseState()` — but `State` struct is in `types.go`
2. **`scope.go`** (3 KB) — all unexported helpers called only by `validate.go`
3. **`dag_bridge.go`** (2 KB) — 2 functions + type alias, naturally part of `depgraph.go`
4. **`retry.go`** (2 KB) — retry logic called by `correct.go`'s `CorrectAndRetry()`
5. **`tiers.go`** (2 KB) — tier selection output of complexity scoring pipeline
6. **`parallelism.go`** (2 KB) — parallelism analysis used by scheduler

File to rename:
7. **`config.go`** → **`resolve.go`** — contains `ResolveExecution()` and `ResolveGate()`, not config loading

## Solution

Perform each merge carefully, preserving all imports and ensuring no duplicate declarations:

### Merge 1: `state.go` → `types.go`
Move `LoadState()`, `SaveState()`, `(*State).SetPhaseState()` and their imports into `types.go` (where `State` struct already lives). The `encoding/json`, `os`, `path/filepath` imports from state.go get added to types.go's import block.

### Merge 2: `scope.go` → `validate.go`
Move `validateScopeOverlaps()`, `scopesOverlap()`, `patternsOverlap()`, `dirContains()`, `isGlob()`, `globsOverlap()`, `globDirPrefix()`, `globSuffixesOverlap()`, `globRepresentative()` into `validate.go`. Merge `scope_test.go` into `validate_test.go`.

### Merge 3: `dag_bridge.go` → `depgraph.go`
Move `Wave` type alias, `NewDAGFromPhases()`, `phasesToDAG()` into `depgraph.go`.

### Merge 4: `retry.go` → `correct.go`
Move `retryWithFeedback()` and `buildRetryPrompt()` into `correct.go`.

### Merge 5: `tiers.go` → `complexity.go`
Move `ModelTier`, `TierConfig`, `DefaultTiers`, `SelectTier()`, `ValidateRouting()` into `complexity.go`. Merge `tiers_test.go` into `complexity_test.go`.

### Merge 6: `parallelism.go` → `scheduler.go`
Move `EffectiveParallelism()` and `WaveParallelism()` into `scheduler.go`. Merge `parallelism_test.go` into `scheduler_test.go`.

### Rename 7: `config.go` → `resolve.go`
Simple `git mv internal/nebula/config.go internal/nebula/resolve.go` and `git mv internal/nebula/config_test.go internal/nebula/resolve_test.go`.

After each merge, deduplicate import blocks (combine stdlib, external, internal groups).

## Files

- `internal/nebula/state.go` — delete (merge into types.go)
- `internal/nebula/types.go` — absorb state.go content
- `internal/nebula/scope.go` — delete (merge into validate.go)
- `internal/nebula/scope_test.go` — delete (merge into validate_test.go)
- `internal/nebula/validate.go` — absorb scope.go content
- `internal/nebula/dag_bridge.go` — delete (merge into depgraph.go)
- `internal/nebula/depgraph.go` — absorb dag_bridge.go content
- `internal/nebula/retry.go` — delete (merge into correct.go)
- `internal/nebula/tiers.go` — delete (merge into complexity.go)
- `internal/nebula/tiers_test.go` — delete (merge into complexity_test.go)
- `internal/nebula/complexity.go` — absorb tiers.go content
- `internal/nebula/parallelism.go` — delete (merge into scheduler.go)
- `internal/nebula/parallelism_test.go` — delete (merge into scheduler_test.go)
- `internal/nebula/scheduler.go` — absorb parallelism.go content
- `internal/nebula/config.go` — rename to resolve.go
- `internal/nebula/config_test.go` — rename to resolve_test.go

## Acceptance Criteria

- [ ] All 6 source files deleted; contents merged into targets
- [ ] `config.go` renamed to `resolve.go`
- [ ] No duplicate type/function declarations
- [ ] Import blocks are clean (no duplicates, properly grouped)
- [ ] `go build ./internal/nebula/...` succeeds
- [ ] `go test ./internal/nebula/...` passes
- [ ] `go vet ./internal/nebula/...` passes
