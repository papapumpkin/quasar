+++
id = "fix-external-imports"
title = "Update cmd/ and tui/ imports to use internal/nebula/worker"
type = "task"
priority = 1
depends_on = ["fix-worker-compilation"]
scope = ["cmd/", "internal/tui/"]
max_review_cycles = 5
max_budget_usd = 10.0
+++

## Problem

After extracting `internal/nebula/worker/`, files in `cmd/` and `internal/tui/` still reference types like `nebula.WorkerGroup`, `nebula.NewWorkerGroup`, `nebula.With*`, etc. that no longer exist in the `nebula` package — they moved to `worker`. The build fails on these external references.

## Solution

### Step 1: Identify all broken references

Run `go build ./cmd/... ./internal/tui/...` and collect all errors. They will all be of the form:
```
nebula.WorkerGroup undefined (type has no field or method...)
```

### Step 2: Update `cmd/` files

For each cmd/ file, add the worker import and update references:

**`cmd/nebula_apply.go`**:
- Add import: `"github.com/aaronsalm/quasar/internal/nebula/worker"`
- Change: `nebula.NewWorkerGroup` → `worker.NewWorkerGroup`
- Change: all `nebula.With*()` options → `worker.With*()`
- Change: `nebula.NewDashboard` → `worker.NewDashboard`
- Change: `nebula.Dashboard` type → `worker.Dashboard`
- Change: `nebula.Gater`/`nebula.GateAction*` → `worker.Gater`/`worker.GateAction*`
- Change: `nebula.NewTerminalGater` → `worker.NewTerminalGater`
- Change: `nebula.Checkpoint` → `worker.Checkpoint`
- Change: `nebula.PhaseRunner` → `worker.PhaseRunner`
- Keep: `nebula.Load`, `nebula.Validate`, `nebula.BuildPlan`, `nebula.Apply` (these stay in parent)

**`cmd/nebula_adapters.go`**:
- Add import: `"github.com/aaronsalm/quasar/internal/nebula/worker"`
- Change: `nebula.PhaseRunnerResult` → `worker.PhaseRunnerResult`
- Change: `nebula.PhaseRunner` → `worker.PhaseRunner`

**`cmd/nebula_status.go`**:
- Add import: `"github.com/aaronsalm/quasar/internal/nebula/worker"`
- Change: `nebula.Metrics` → `worker.Metrics`
- Change: `nebula.LoadMetrics` → `worker.LoadMetrics`
- Change: `nebula.LoadMetricsWithHistory` → `worker.LoadMetricsWithHistory`
- Change: `nebula.HistorySummary` → `worker.HistorySummary`

**`cmd/tui.go`**:
- Add import: `"github.com/aaronsalm/quasar/internal/nebula/worker"`
- Same changes as nebula_apply.go (WorkerGroup, With*, Dashboard, Gater, etc.)

### Step 3: Update `internal/tui/` files

**`internal/tui/gater.go`**:
- Add import: `"github.com/aaronsalm/quasar/internal/nebula/worker"`
- Change: `nebula.GateAction` → `worker.GateAction`
- Change: `nebula.GatePrompter` → `worker.GatePrompter`
- Change: `nebula.Checkpoint` → `worker.Checkpoint`
- Change: `nebula.GateActionAccept` etc. → `worker.GateActionAccept`

**`internal/tui/gateprompt.go`**:
- Add import: `"github.com/aaronsalm/quasar/internal/nebula/worker"`
- Change: `nebula.GateAction` → `worker.GateAction`
- Change: `nebula.Checkpoint` → `worker.Checkpoint`
- Change: `nebula.FileChange` → `worker.FileChange`

**`internal/tui/msg.go`**:
- Check for any references to moved types (Checkpoint, GateAction, etc.)
- Add worker import if needed

**`internal/tui/overlay.go`**:
- Check if it references `nebula.WorkerResult` (this stays in nebula, no change needed)
- Check for any other moved types

**`internal/tui/planview.go`**:
- Check for `nebula.ExecutionPlan` (stays in nebula, no change)

### Step 4: Iterate until clean

Run `go build ./...` and fix remaining errors. If removing the nebula import from a file that no longer uses any nebula types, remove it to avoid unused import errors.

### Step 5: Format

Run `goimports -w cmd/ internal/tui/` to clean up import blocks.

## Files

- `cmd/nebula_apply.go` — update imports and type references
- `cmd/nebula_adapters.go` — update imports and type references
- `cmd/nebula_status.go` — update imports and type references
- `cmd/tui.go` — update imports and type references
- `internal/tui/gater.go` — update imports and type references
- `internal/tui/gateprompt.go` — update imports and type references
- `internal/tui/msg.go` — verify/update imports
- `internal/tui/overlay.go` — verify/update imports

## Acceptance Criteria

- [ ] `go build ./...` succeeds with zero errors
- [ ] `go test ./...` passes
- [ ] `go vet ./...` passes
- [ ] `goimports` applied — no formatting changes needed
- [ ] No unused imports remain
- [ ] `nebula` import retained only in files that still use parent-package types
