+++
id = "fix-worker-compilation"
title = "Fix compilation errors in internal/nebula/worker/ files"
type = "task"
priority = 1
depends_on = ["extract-worker-subpackage"]
scope = ["internal/nebula/worker/"]
max_review_cycles = 7
max_budget_usd = 15.0
+++

## Problem

After moving files to `internal/nebula/worker/`, the worker package won't compile because files reference types from the parent `nebula` package without the `nebula.` prefix. Every reference to a type that stayed in `internal/nebula/` needs to be qualified.

## Solution

### Step 1: Add the nebula import to every worker/ file

Every `.go` file in `internal/nebula/worker/` that references parent types needs this import:

```go
import (
    "github.com/aaronsalm/quasar/internal/nebula"
)
```

### Step 2: Prefix all parent-package type references with `nebula.`

These types STAYED in `internal/nebula/` and must be prefixed with `nebula.` whenever referenced in worker/ files:

**Structs/Types**: `PhaseSpec`, `Nebula`, `State`, `PhaseState`, `Manifest`, `Execution`, `Defaults`, `WorkerResult`, `Plan`, `Action`, `GateMode`, `ValidationError`, `Info`, `Context`, `Dependencies`, `ResolvedExecution`, `RoutingContext`, `DependencyInferrer`, `InferenceResult`, `DepEdge`, `Wave`, `WriteOptions`, `PlanEngine`, `ExecutionPlan`, `PlanRisk`, `PlanStats`, `PlanChange`, `ArchitectRequest`, `ArchitectResult`, `ArchitectMode`, `GenerateRequest`, `GenerateResult`, `DecomposeOp`, `SubPhaseEntry`, `ComplexitySignals`, `ComplexityResult`, `ModelTier`, `TierConfig`, `CodebaseAnalysis`, `PackageSummary`

**Constants**: `PhasePending`, `PhaseCreated`, `PhaseInProgress`, `PhaseDone`, `PhaseFailed`, `PhaseSkipped`, `PhaseDecomposed`, `ActionCreate`, `ActionUpdate`, `ActionSkip`, `ActionClose`, `ActionRetry`, `GateModeTrust`, `GateModeReview`, `GateModeApprove`, `GateModeWatch`, `DefaultMaxReviewCycles`, `DefaultMaxBudgetUSD`

**Functions**: `LoadState()`, `SaveState()`, `Load()`, `Validate()`, `ValidateHotAdd()`, `BuildPlan()`, `Apply()`, `NewDAGFromPhases()`, `PhasesByID()`, `MarshalPhaseFile()`, `ResolveExecution()`, `ResolveGate()`, `ScoreComplexity()`, `BuildComplexitySignals()`, `SelectTier()`, `CheckDependencies()`, `RenderPlan()`, `NewGitCommitter()`, `NewGitCommitterWithBranch()`, `PostCompletion()`, `InterventionFileNames()`, `GitExcludePatterns()`, `NewBranchManager()`, `RunArchitect()`, `CorrectAndRetry()`

**Sentinel errors**: `ErrNoManifest`, `ErrDuplicateID`, `ErrDependencyCycle`, `ErrUnknownDep`, `ErrMissingField`, `ErrUnmetDependency`, `ErrManualStop`, `ErrInvalidGate`, `ErrPlanRejected`, `ErrScopeOverlap`, `ErrPhaseAlreadyStarted`, `ErrPlanHasErrors`

### Step 3: Remove `nebula.` prefix from same-package references

Types that MOVED to `worker/` should NOT have a `nebula.` prefix. They are now in the same package. These include: `WorkerGroup`, `Scheduler`, `PhaseTracker`, `Gater`, `GatePrompter`, `GateAction`, `Metrics`, `PhaseMetrics`, `WaveMetrics`, `Dashboard`, `Watcher`, `HotReloader`, `ProgressReporter`, `Checkpoint`, `FileChange`, `PhaseRunner`, `PhaseRunnerResult`, `Option`, etc.

### Step 4: Fix test files too

Test files in `worker/` will also reference parent types. Apply the same `nebula.` prefixing to all `*_test.go` files.

### Step 5: Iterate until clean

Run `go build ./internal/nebula/worker/...` repeatedly and fix each error. The compiler will tell you exactly which references need updating. Do NOT try to fix cmd/ or tui/ imports — that's the next phase.

## Files

- All `.go` files in `internal/nebula/worker/` — add nebula import, prefix parent types
- All `*_test.go` files in `internal/nebula/worker/` — same updates

## Acceptance Criteria

- [ ] `go build ./internal/nebula/...` succeeds (both parent and worker/)
- [ ] `go build ./internal/nebula/worker/...` succeeds specifically
- [ ] `go test ./internal/nebula/worker/...` passes
- [ ] No references to unqualified parent types remain in worker/ files
- [ ] `go vet ./internal/nebula/...` passes
