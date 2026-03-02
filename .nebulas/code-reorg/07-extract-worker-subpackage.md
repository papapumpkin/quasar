+++
id = "extract-worker-subpackage"
title = "Move files to internal/nebula/worker/ and update package declarations"
type = "task"
priority = 1
depends_on = ["consolidate-nebula", "consolidate-tui", "split-nebula-healing"]
scope = ["internal/nebula/"]
max_review_cycles = 5
max_budget_usd = 10.0
+++

## Problem

`internal/nebula/` has too many files mixing parse-time and execution-time concerns. We need to create the `internal/nebula/worker/` sub-package by physically moving files and updating their package declarations.

This phase does ONLY the mechanical file moves. Compilation will be broken after this phase — that's expected and fixed in the next phase.

## Solution

Execute these exact steps:

### Step 1: Create the directory

```bash
mkdir -p internal/nebula/worker
```

### Step 2: Move source files (use git mv for each)

```bash
git mv internal/nebula/worker.go internal/nebula/worker/worker.go
git mv internal/nebula/worker_exec.go internal/nebula/worker/exec.go
git mv internal/nebula/worker_fabric.go internal/nebula/worker/fabric.go
git mv internal/nebula/worker_options.go internal/nebula/worker/options.go
git mv internal/nebula/worker_healing.go internal/nebula/worker/healing.go
git mv internal/nebula/scheduler.go internal/nebula/worker/scheduler.go
git mv internal/nebula/tracker.go internal/nebula/worker/tracker.go
git mv internal/nebula/gate.go internal/nebula/worker/gate.go
git mv internal/nebula/progress.go internal/nebula/worker/progress.go
git mv internal/nebula/metrics.go internal/nebula/worker/metrics.go
git mv internal/nebula/metrics_store.go internal/nebula/worker/metrics_store.go
git mv internal/nebula/dashboard.go internal/nebula/worker/dashboard.go
git mv internal/nebula/watcher.go internal/nebula/worker/watcher.go
git mv internal/nebula/hotreload.go internal/nebula/worker/hotreload.go
git mv internal/nebula/decompose.go internal/nebula/worker/decompose.go
git mv internal/nebula/decompose_dag.go internal/nebula/worker/decompose_dag.go
git mv internal/nebula/healing.go internal/nebula/worker/failure_diagnosis.go
git mv internal/nebula/healing_remediate.go internal/nebula/worker/healing_remediate.go
git mv internal/nebula/checkpoint.go internal/nebula/worker/phase_checkpoint.go
```

### Step 3: Move test files

Move ALL corresponding test files for the source files above. For each `*_test.go` that matches a moved source file, `git mv` it to `internal/nebula/worker/`. Apply the same renames (strip `worker_` prefix, rename `healing_test.go` → `failure_diagnosis_test.go`, `checkpoint_test.go` → `phase_checkpoint_test.go`).

Example:
```bash
git mv internal/nebula/worker_exec_test.go internal/nebula/worker/exec_test.go
git mv internal/nebula/scheduler_test.go internal/nebula/worker/scheduler_test.go
# ... etc for all test files of moved source files
```

### Step 4: Update package declarations

In EVERY `.go` file now in `internal/nebula/worker/` (both source and test), change:
```go
package nebula
```
to:
```go
package worker
```

Use sed or manual editing. This is a simple text replacement at the top of each file.

### Step 5: DO NOT try to fix compilation errors

After these moves, `go build` WILL fail because:
- worker/ files reference nebula types without the `nebula.` prefix
- cmd/ and tui/ files reference types that moved packages

This is expected. The next phase handles all compilation fixes.

## Files

- `internal/nebula/worker/` — create directory with 19 source files + test files
- `internal/nebula/` — 19 source files removed
- All moved files — package declaration changed to `package worker`

## Acceptance Criteria

- [ ] `internal/nebula/worker/` directory exists
- [ ] 19 source files are in `internal/nebula/worker/`
- [ ] All moved files have `package worker` declaration
- [ ] All corresponding test files are also moved
- [ ] `internal/nebula/` retains only: types.go, errors.go, parse.go, validate.go, depgraph.go, resolve.go, plan.go, plan_engine.go, apply.go, writer.go, architect.go, generate.go, correct.go, analyze.go, complexity.go, git.go, branch.go (and their test files)
- [ ] No source files were accidentally left behind or duplicated
