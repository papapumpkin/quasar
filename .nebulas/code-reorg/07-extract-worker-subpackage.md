+++
id = "extract-worker-subpackage"
title = "Extract internal/nebula/worker/ sub-package for execution-time orchestration"
type = "task"
priority = 1
depends_on = ["consolidate-nebula", "consolidate-tui", "split-nebula-healing"]
scope = ["internal/nebula/", "cmd/", "internal/tui/"]
max_review_cycles = 7
max_budget_usd = 15.0
+++

## Problem

`internal/nebula/` has ~37 files (after prior consolidation phases) spanning two very different concerns:
1. **Parse-time**: types, parsing, validation, planning, generation, git operations
2. **Execution-time**: worker orchestration, scheduling, tracking, gating, metrics, dashboard, file watching, healing, decomposition, checkpoints

These concerns have a clean API boundary: `WorkerGroup` is only created by `cmd/` code, and nothing in the parent nebula package calls WorkerGroup methods. The dependency flows one way: worker code imports nebula types, never the reverse.

## Solution

Create `internal/nebula/worker/` sub-package and move all execution-time files there.

### Step 1: Create directory and move files

Create `internal/nebula/worker/`. Move these files (with renames where noted):

| Source | Destination | Notes |
|--------|-------------|-------|
| `nebula/worker.go` | `worker/worker.go` | Core WorkerGroup |
| `nebula/worker_exec.go` | `worker/exec.go` | Strip `worker_` prefix |
| `nebula/worker_fabric.go` | `worker/fabric.go` | Strip prefix |
| `nebula/worker_options.go` | `worker/options.go` | Strip prefix |
| `nebula/worker_healing.go` | `worker/healing.go` | Strip prefix |
| `nebula/scheduler.go` | `worker/scheduler.go` | |
| `nebula/tracker.go` | `worker/tracker.go` | |
| `nebula/gate.go` | `worker/gate.go` | |
| `nebula/progress.go` | `worker/progress.go` | |
| `nebula/metrics.go` | `worker/metrics.go` | |
| `nebula/metrics_store.go` | `worker/metrics_store.go` | |
| `nebula/dashboard.go` | `worker/dashboard.go` | |
| `nebula/watcher.go` | `worker/watcher.go` | |
| `nebula/hotreload.go` | `worker/hotreload.go` | |
| `nebula/decompose.go` | `worker/decompose.go` | |
| `nebula/decompose_dag.go` | `worker/decompose_dag.go` | |
| `nebula/healing.go` | `worker/failure_diagnosis.go` | Rename to avoid conflict |
| `nebula/healing_remediate.go` | `worker/healing_remediate.go` | |
| `nebula/checkpoint.go` | `worker/phase_checkpoint.go` | Rename to avoid confusion with internal/checkpoint/ |

Move corresponding test files too — ALL `*_test.go` files for the moved source files go to `worker/`.

### Step 2: Update package declarations

In every moved file, change `package nebula` → `package worker`.

### Step 3: Update internal references in moved files

Within `worker/` files:
- References to types that STAY in `nebula` (e.g., `PhaseSpec`, `Nebula`, `State`, `Manifest`, `WorkerResult`, `GateMode`, `PhaseState` constants, `SaveState()`, `Validate()`, `ValidateHotAdd()`, etc.) must be prefixed with `nebula.` and the import `"github.com/aaronsalm/quasar/internal/nebula"` added.
- References to types that MOVED to `worker` (e.g., `WorkerGroup`, `Scheduler`, `PhaseTracker`, `Gater`, `Metrics`, etc.) lose their `nebula.` prefix since they're now in the same package.

### Step 4: Update external imports

**`cmd/` files** — add `worker "github.com/aaronsalm/quasar/internal/nebula/worker"` import:
- `cmd/nebula_apply.go`: `nebula.NewWorkerGroup` → `worker.NewWorkerGroup`, all `nebula.With*` → `worker.With*`, `nebula.NewDashboard` → `worker.NewDashboard`, `nebula.Dashboard` → `worker.Dashboard`
- `cmd/nebula_adapters.go`: `nebula.PhaseRunnerResult` → `worker.PhaseRunnerResult`, `nebula.PhaseRunner` → `worker.PhaseRunner`
- `cmd/nebula_status.go`: `nebula.Metrics` → `worker.Metrics`, `nebula.LoadMetrics` → `worker.LoadMetrics`, `nebula.LoadMetricsWithHistory` → `worker.LoadMetricsWithHistory`, `nebula.HistorySummary` → `worker.HistorySummary`
- `cmd/tui.go`: same pattern as nebula_apply.go

**`internal/tui/` files** — add worker import where types moved:
- `tui/gater.go`: `nebula.GateAction` → `worker.GateAction`, `nebula.GatePrompter` → `worker.GatePrompter`, `nebula.Checkpoint` → `worker.Checkpoint`
- `tui/gateprompt.go`: `nebula.GateAction` → `worker.GateAction`, `nebula.Checkpoint` → `worker.Checkpoint`
- `tui/msg.go`: update any references to types that moved to worker
- `tui/overlay.go`: check if it references moved types

### Step 5: Type placement guide

**Stays in `nebula`** (parse/plan time types):
`PhaseSpec`, `Nebula`, `State`, `PhaseState`, `Manifest`, `Execution`, `Defaults`, `WorkerResult`, `Plan`, `Action`, `GateMode`, `ValidationError`, sentinel errors, `LoadState()`, `SaveState()`, `Load()`, `Validate()`, `BuildPlan()`, `Apply()`

**Moves to `worker`** (execution-time types):
`WorkerGroup`, `Option`, `With*()`, `PhaseRunner`, `PhaseRunnerResult`, `GateAction`, `Gater`, `GatePrompter`, `Checkpoint`, `FileChange`, `Scheduler`, `PhaseTracker`, `Metrics`, `PhaseMetrics`, `WaveMetrics`, `HistorySummary`, `Dashboard`, `Watcher`, `HotReloader`, `ProgressReporter`, `ProgressFunc`, `HotAddFunc`, `FailureDiagnosis`, `HealingPolicy`, `DecomposeResult`, `DecomposeFinding`

### Step 6: Verify

Run `go build ./...` to catch any missed import updates. The compiler will flag every reference error. Fix iteratively until clean.

## Files

- `internal/nebula/worker/` — create directory, 19 source files + corresponding test files
- `internal/nebula/` — 19 source files removed (17 remain)
- `cmd/nebula_apply.go` — update imports
- `cmd/nebula_adapters.go` — update imports
- `cmd/nebula_status.go` — update imports
- `cmd/tui.go` — update imports
- `internal/tui/gater.go` — update imports
- `internal/tui/gateprompt.go` — update imports
- `internal/tui/msg.go` — update imports
- `internal/tui/overlay.go` — verify/update imports

## Acceptance Criteria

- [ ] `internal/nebula/worker/` directory exists with 19 source files
- [ ] `internal/nebula/` has ~17 source files (types, parsing, validation, planning, generation, git)
- [ ] All moved files have `package worker`
- [ ] No circular imports: `worker` imports `nebula`, `nebula` does NOT import `worker`
- [ ] All `cmd/` files compile with updated imports
- [ ] All `tui/` files compile with updated imports
- [ ] `go build ./...` succeeds with zero errors
- [ ] `go test ./...` passes
- [ ] `go vet ./...` passes
- [ ] `goimports` applied to all modified files
