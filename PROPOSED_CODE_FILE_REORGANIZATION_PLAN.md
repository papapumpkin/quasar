# Proposed Code File Reorganization Plan

## Executive Summary

This document proposes a reorganization of the `internal/` directory in the Quasar codebase. The primary goals are:

1. **Delete dead code** (empty stubs, placeholder files)
2. **Consolidate tiny related files** that should live together
3. **Split the 44-file `internal/nebula/` mega-package** into focused sub-packages
4. **Resolve naming ambiguities** between packages
5. **Maintain all existing functionality** — no behavioral changes

The reorganization follows Go conventions: prefer fewer, larger packages with clear boundaries; only create sub-packages where there's a genuine API boundary; keep shared types in parent packages.

---

## Table of Contents

1. [Current State Analysis](#1-current-state-analysis)
2. [Proposed Changes](#2-proposed-changes)
   - [Tier 1: Dead Code Deletion](#tier-1-dead-code-deletion)
   - [Tier 2: File Consolidation](#tier-2-file-consolidation)
   - [Tier 3: File Splitting](#tier-3-file-splitting-large-files)
   - [Tier 4: Nebula Sub-Package Extraction](#tier-4-nebula-sub-package-extraction)
   - [Tier 5: Naming Clarifications](#tier-5-naming-clarifications)
3. [Import Change Tracker](#3-import-change-tracker)
4. [Files NOT Changed (and Why)](#4-files-not-changed-and-why)
5. [Migration Strategy](#5-migration-strategy)
6. [Risk Assessment](#6-risk-assessment)

---

## 1. Current State Analysis

### Package Inventory (19 packages)

| Package | Files (non-test) | Files (test) | Purpose | Internal Imports |
|---------|-----------------|-------------|---------|-----------------|
| `agent` | 5 | 2 | Agent roles, prompts, Invoker interface | None |
| `ansi` | 1 | 0 | ANSI escape code constants | None |
| `arch_test` | 0 | 6 | Architecture enforcement tests | N/A |
| `beads` | 2 | 1 | Beads CLI wrapper | None |
| **`board`** | **7 (all empty)** | **6** | **Dead code — replaced by fabric** | **None** |
| `checkpoint` | 2 | 2 | Loop state serialization for resume | `loop` |
| `claude` | 3 | 1 | Claude CLI invoker | `agent` |
| `config` | 1 | 1 | Viper-based config loading | None |
| `dag` | 8 | 6 | DAG algorithms (toposort, PageRank, etc.) | None |
| `fabric` | 11 | 9 | Coordination substrate (SQLite + contracts) | `agent` |
| `filter` | 3 | 2 | Pre-reviewer deterministic checks | None |
| `loop` | 17 | 17 | Coder-reviewer cycle state machine | `agent`, `beads`, `fabric`, `filter`, `ui` |
| **`nebula`** | **44** | **36** | **Multi-task orchestration (MASSIVE)** | `agent`, `ansi`, `beads`, `checkpoint`, `claude`, `config`, `dag`, `fabric`, `loop`, `snapshot`, `telemetry`, `tycho`, `ui` |
| `neutron` | 2 | 2 | Epoch archival to SQLite | `fabric` |
| `snapshot` | 3 | 3 | Project context scanning | None |
| `telemetry` | 1 | 1 | JSONL event stream | None |
| `tui` | 37 | 33 | BubbleTea terminal UI | `dag`, `fabric`, `loop`, `nebula`, `telemetry`, `tycho`, `ui` |
| `tycho` | 2 | 2 | DAG+Fabric scheduler bridge | `dag`, `fabric` |
| `ui` | 4 | 3 | Stderr printer (ANSI colors) | `ansi`, `dag` |

### Key Problems Identified

1. **`internal/board/`** — 7 completely empty stub files (14 bytes each, just `package board`) plus 6 test files. Replaced by `internal/fabric/` on Feb 23, 2026. Not imported anywhere. Pure dead weight.

2. **`internal/nebula/`** — 44 non-test source files spanning 18 distinct functional areas (types, parsing, validation, planning, worker orchestration, scheduling, gating, decomposition, healing, checkpoints, git, metrics, file watching, dashboard, analysis, architect/generation, persistence, and error handling). This is the biggest organizational problem in the codebase.

3. **`internal/nebula/worker_board.go`** — Empty placeholder file (0 symbols). Never used.

4. **`internal/tui/architect_overlay.go`** — Empty placeholder file. Never used.

5. **Naming ambiguity** — `internal/nebula/config.go` vs `internal/config/config.go` and `internal/nebula/checkpoint.go` vs `internal/checkpoint/checkpoint.go` create confusion about where configuration/checkpoint logic lives.

6. **Tiny files that should be consolidated** — Several files under 25 lines that are really part of a larger logical unit (e.g., `finding_id.go` at 20 lines, `errors.go` at 12 lines in loop/).

7. **Large files that could benefit from splitting** — `loop/loop.go` at 1000+ lines, `nebula/worker.go` at 500+ lines.

### Dependency Graph (No Circular Dependencies)

```
Layer 0 (leaf packages, no internal imports):
  agent, ansi, beads, config, dag, filter, snapshot, telemetry

Layer 1 (one-hop imports):
  claude      → agent
  ui          → ansi, dag
  fabric      → agent

Layer 2:
  tycho       → dag, fabric
  neutron     → fabric
  loop        → agent, beads, fabric, filter, ui
  checkpoint  → loop

Layer 3 (orchestrators):
  nebula      → agent, ansi, beads, checkpoint, claude, config, dag,
                 fabric, loop, snapshot, telemetry, tycho, ui

Layer 4 (presentation):
  tui         → dag, fabric, loop, nebula, telemetry, tycho, ui

Layer 5 (composition root):
  cmd/        → (wires everything together)
```

---

## 2. Proposed Changes

### Tier 1: Dead Code Deletion

These are unambiguous wins — no code references these files.

#### 1a. Delete `internal/board/` entirely

**Rationale**: The entire `board` package was renamed to `fabric` on Feb 23, 2026 (commits 1e6cd62, 083227b). All 7 `.go` files are empty stubs containing only `package board`. The 6 test files are also dead. The package is not imported anywhere in the codebase.

**Files to delete** (13 total):
- `internal/board/board.go` (14 bytes, empty stub)
- `internal/board/contract.go` (14 bytes, empty stub)
- `internal/board/llmpoller.go` (14 bytes, empty stub)
- `internal/board/poller.go` (14 bytes, empty stub)
- `internal/board/publisher.go` (14 bytes, empty stub)
- `internal/board/pushback.go` (14 bytes, empty stub)
- `internal/board/sqlite.go` (14 bytes, empty stub)
- `internal/board/publisher_test.go`
- `internal/board/pushback_test.go`
- `internal/board/poller_test.go`
- `internal/board/llmpoller_test.go`
- `internal/board/sqlite_test.go`
- `internal/board/integration_test.go`

**Import changes**: None. No file imports `internal/board`.

#### 1b. Delete `internal/nebula/worker_board.go`

**Rationale**: Empty file with 0 symbols. Was a placeholder for board integration that was replaced by `worker_fabric.go`.

**Import changes**: None.

#### 1c. Delete `internal/tui/architect_overlay.go`

**Rationale**: Empty placeholder file. No types, no functions, no content.

**Import changes**: None.

---

### Tier 2: File Consolidation

These merge very small files into their natural parent file, reducing file count without losing clarity.

#### 2a. `internal/loop/`: Consolidate finding-related micro-files

**Current state**: The "finding" subsystem spans 4 files:
- `finding_id.go` — 20 lines, just `FindingID()` function (SHA-256 hash)
- `finding_lifecycle.go` — 50 lines, `ApplyVerifications()` + `LifecycleSummary`
- `finding_serialize.go` — 25 lines, `SerializeFindings()` function
- `parse.go` — 200+ lines, parses ISSUE/VERIFICATION/APPROVED/REPORT blocks

**Proposed**: Merge `finding_id.go`, `finding_lifecycle.go`, and `finding_serialize.go` into a single `findings.go` file.

**Rationale**: These three files total ~95 lines and form a single logical unit: finding identity, lifecycle tracking, and serialization. Keeping them as three separate files creates unnecessary indirection. The `parse.go` file stays separate because it handles broader output parsing (not just findings).

**New file**: `internal/loop/findings.go` — Contains `FindingID()`, `LifecycleSummary`, `ApplyVerifications()`, `SerializeFindings()`

**Test files**: Merge `finding_id_test.go`, `finding_lifecycle_test.go`, `finding_serialize_test.go` into `findings_test.go`.

**Import changes**: None (all within same package).

#### 2b. `internal/loop/`: Merge `errors.go` into `state.go`

**Current state**: `errors.go` is 12 lines containing only two sentinel errors (`ErrMaxCycles`, `ErrBudgetExceeded`).

**Proposed**: Move the two sentinel error variables into the top of `state.go`, which already defines the state types and phase constants that these errors relate to.

**Rationale**: Two `var` declarations don't warrant their own file. These errors are intrinsic to the loop state machine (max cycles reached = phase transition, budget exceeded = phase transition). `state.go` already has all the related constants.

**Import changes**: None (same package).

#### 2c. `internal/nebula/`: Merge `state.go` into `types.go`

**Current state**: `state.go` (3 KB) defines `LoadState()`, `SaveState()`, and `(*State).SetPhaseState()`. The `State` type itself is already defined in `types.go`.

**Proposed**: Merge `state.go` into `types.go`.

**Rationale**: The `State` struct lives in `types.go`, but its methods and persistence functions live in `state.go`. This split creates confusion — when looking for state-related code you need to check two files. Since both files are relatively small and tightly coupled (state.go has no additional imports beyond stdlib), consolidation is natural.

**Import changes**: None (same package).

#### 2d. `internal/nebula/`: Merge `scope.go` into `validate.go`

**Current state**: `scope.go` (3 KB) contains `validateScopeOverlaps()` and helper functions for glob pattern matching. All functions are unexported. The only caller is `validate.go`.

**Proposed**: Merge `scope.go` into `validate.go`.

**Rationale**: `scope.go` contains internal helper functions called exclusively by `validate.go`. They're logically part of validation. The combined file remains under 300 lines, well within Go conventions.

**Import changes**: None (same package). Merge `scope_test.go` into `validate_test.go`.

#### 2e. `internal/nebula/`: Merge `dag_bridge.go` into `depgraph.go`

**Current state**: `dag_bridge.go` (2 KB) has only 2 functions: `NewDAGFromPhases()` and `phasesToDAG()`, plus a type alias `Wave = dag.Wave`.

**Proposed**: Merge into `depgraph.go`, which already handles dependency graph construction.

**Rationale**: Both files deal with constructing DAGs from phase specs. `dag_bridge.go` is too small to justify its own file, and its functions are conceptually part of dependency graph management.

**Import changes**: None (same package).

#### 2f. `internal/nebula/`: Merge `retry.go` into `correct.go`

**Current state**: `retry.go` (2 KB) has `retryWithFeedback()` and `buildRetryPrompt()`. `correct.go` (5 KB) has `CorrectAndRetry()` plus auto-correction logic.

**Proposed**: Merge `retry.go` into `correct.go`.

**Rationale**: Both files handle the "fix errors and retry" concern. `CorrectAndRetry()` in `correct.go` already coordinates the retry flow. The retry prompt building is part of the same logical process. Combined file stays under 300 lines.

**Import changes**: None (same package).

#### 2g. `internal/nebula/`: Merge `tiers.go` into `complexity.go`

**Current state**: `tiers.go` (2 KB) defines `ModelTier`, `TierConfig`, `DefaultTiers`, `SelectTier()`, and `ValidateRouting()`. `complexity.go` (4 KB) computes complexity scores that feed into tier selection.

**Proposed**: Merge `tiers.go` into `complexity.go`.

**Rationale**: Tiers are the output of complexity scoring. The flow is `BuildComplexitySignals() → ScoreComplexity() → SelectTier()`. Having these in separate files breaks a linear pipeline. Combined file is ~250 lines.

**Import changes**: None (same package).

#### 2h. `internal/nebula/`: Merge `parallelism.go` into `scheduler.go`

**Current state**: `parallelism.go` (2 KB) has `EffectiveParallelism()` and `WaveParallelism()`. `scheduler.go` (5 KB) handles ready task scheduling and track computation.

**Proposed**: Merge `parallelism.go` into `scheduler.go`.

**Rationale**: Both deal with execution parallelism analysis. `EffectiveParallelism()` uses the scheduler's track count. They share the same import (`internal/dag`) and the same conceptual domain.

**Import changes**: None (same package).

#### 2i. `internal/tui/`: Merge `logo.go` into `banner.go`

**Current state**: `logo.go` (~30 lines) defines `Logo()` and `LogoPlain()` — styled single-line text. `banner.go` (~200 lines) renders the full ASCII art banner in multiple sizes.

**Proposed**: Merge `logo.go` into `banner.go`.

**Rationale**: The logo is a component of the banner display. Both deal with the Quasar brand rendering. Having a separate file for 2 tiny functions is unnecessary. Merge `logo_test.go` into `banner_test.go`.

**Import changes**: None (same package).

### Summary of Tier 2

After consolidation, file count changes:

| Package | Before | After | Delta |
|---------|--------|-------|-------|
| `loop` (non-test) | 17 | 14 | -3 |
| `nebula` (non-test) | 44 | 37 | -7 |
| `tui` (non-test) | 37 | 36 | -1 |
| **Total** | **98** | **87** | **-11** |

---

### Tier 3: File Splitting (Large Files)

These split oversized files into focused units for better navigability.

#### 3a. Split `internal/loop/loop.go` (~1000 lines)

**Current state**: This single file contains the entire coder-reviewer cycle engine: task creation, coder phase, reviewer phase, filter-fix loops, budget tracking, approval handling, prompt caching, finding bead creation, hail extraction, cycle commit sealing, and refactor draining.

**Proposed split**:

| New File | Functions Moved | Purpose |
|----------|----------------|---------|
| `loop.go` (stays) | `Loop` struct, `RunTask()`, `RunExistingTask()`, `RunFromCheckpoint()`, main cycle loop, `checkBudget()`, `perAgentBudget()`, `emit()`, `handleApproval()`, `sealCycleSHA()`, `finalCommitSHA()` | Core orchestration and lifecycle |
| `loop_coder.go` (new) | `runCoderPhase()`, `runLintFixLoop()`, `runFilterChecks()`, `runFilterFixLoop()`, `drainRefactor()` | Coder execution: invoke agent, run lint, run filters, handle fixes |
| `loop_reviewer.go` (new) | `runReviewerPhase()`, `emitCycleSummary()`, `extractAndPostHails()`, `postMaxCyclesHail()`, `createFindingBeads()`, `emitBeadUpdate()` | Reviewer execution: invoke reviewer, extract findings, emit results |
| `loop_cache.go` (new) | `cacheSystemPrompts()`, `trackCacheMetrics()` | Prompt caching strategy (stable prefix computation) |

**Rationale**: At 1000+ lines, `loop.go` requires significant scrolling to find specific phases. The coder and reviewer phases are distinct execution paths that are conceptually separate. Prompt caching is an optimization concern that clutters the main loop logic.

After split, `loop.go` drops to ~400 lines (the core orchestration), each new file is 150-250 lines.

**Import changes**: None (same package, just file reorganization).

#### 3b. Split `internal/nebula/healing.go` (~10 KB / 300+ lines)

**Current state**: Contains both failure **diagnosis** (analyzing what went wrong) and **remediation** (generating a healing phase). These are distinct steps.

**Proposed split**:

| New File | Functions Moved | Purpose |
|----------|----------------|---------|
| `healing.go` (stays) | `FailureDiagnosis`, `FailureKind`, `FailureContext`, `HealingPolicy`, `AnalyzeFailure()`, `HealingSummary()`, `HealingContext()`, classification helpers | Failure analysis and diagnosis |
| `healing_remediate.go` (new) | `PartialWork`, `BuildPartialWork()`, `InsertRemediationPhase()`, `BuildRemediationRequest()`, `FinalizeRemediationSpec()`, `GitDiffLister` interface | Remediation phase generation and insertion |

**Rationale**: Diagnosis and remediation are separate concerns with different consumers. The split makes the flow clearer: diagnose first, then decide whether to remediate. Each file stays at ~150 lines.

**Import changes**: None (same package).

---

### Tier 4: Nebula Sub-Package Extraction

This is the most impactful change. The `internal/nebula/` package has 37 files (after Tier 2 consolidation) spanning very different concerns. We extract two natural sub-packages where there are clean API boundaries.

#### 4a. Extract `internal/nebula/worker/` — Phase orchestration engine

**Rationale**: The worker subsystem (WorkerGroup and its extensions) is the largest functional group in nebula (currently 5 `worker_*.go` files plus scheduler, tracker, gate, progress, dashboard, watcher, hotreload — ~15 files). It has a clear API boundary:

- **Input**: `WorkerGroup` is created with `NewWorkerGroup()` using an options builder pattern
- **Output**: `[]WorkerResult` from `wg.Run()`
- **Consumers**: Only `cmd/nebula_apply.go` and `cmd/tui.go` create WorkerGroups
- **Critical check**: Nothing in the parent `nebula` package calls WorkerGroup methods. The flow is always `cmd/ → nebula/worker`, not `nebula → nebula/worker`. This means **no circular imports**.

**Files to move into `internal/nebula/worker/`**:

| File | Rename? | Reason for inclusion |
|------|---------|---------------------|
| `worker.go` | No | Core WorkerGroup struct and Run() |
| `worker_exec.go` | `exec.go` | Phase execution, interventions |
| `worker_fabric.go` | `fabric.go` | Fabric/entanglement integration |
| `worker_options.go` | `options.go` | Builder pattern options, PhaseRunner interface |
| `worker_healing.go` | `healing.go` | Worker-side healing integration |
| `scheduler.go` | No | DAG-aware phase scheduling |
| `tracker.go` | No | Phase state tracking during execution |
| `gate.go` | No | Gating strategies (trust/review/approve/watch) |
| `progress.go` | No | Progress reporting and state saves |
| `metrics.go` | No | In-memory metrics collection |
| `metrics_store.go` | No | Metrics persistence to disk |
| `dashboard.go` | No | Live progress dashboard |
| `watcher.go` | No | File system monitoring |
| `hotreload.go` | No | Hot-add/refactor handling |
| `decompose.go` | No | Phase decomposition orchestration |
| `decompose_dag.go` | No | DAG surgery for decomposition |
| `healing.go` | `failure_diagnosis.go` | Failure analysis (rename to avoid conflict with worker's healing.go) |
| `healing_remediate.go` | No | Remediation phase generation (from Tier 3 split) |
| `checkpoint.go` | `phase_checkpoint.go` | Phase checkpoint capture/render (rename to avoid conflict with `internal/checkpoint/`) |

**That's 19 files moving to the sub-package, leaving 18 in the parent.**

**What stays in `internal/nebula/` (parent)**:

| File | Purpose |
|------|---------|
| `types.go` | All shared types (Nebula, PhaseSpec, State, Manifest, WorkerResult, etc.) |
| `errors.go` | Sentinel errors and ValidationError |
| `parse.go` | Load and parse nebula manifests + phase files |
| `validate.go` | Structural validation (includes merged scope.go) |
| `depgraph.go` | Dependency inference (includes merged dag_bridge.go) |
| `config.go` → rename to `resolve.go` | Execution config resolution |
| `plan.go` | Basic plan generation |
| `plan_engine.go` | Advanced planning with risk assessment |
| `apply.go` | Execute plan actions (create/update/close beads) |
| `writer.go` | Write nebula to disk |
| `architect.go` | LLM architect agent for phase design |
| `generate.go` | Full nebula generation from prompt |
| `correct.go` | Auto-correction of validation errors (includes merged retry.go) |
| `analyze.go` | Codebase analysis for architect context |
| `complexity.go` | Complexity scoring + tier selection (includes merged tiers.go) |
| `git.go` | Git committer for phase-boundary commits |
| `branch.go` | Branch management for nebula isolation |

**17 files** — a manageable, focused package covering types, parsing, validation, planning, generation, and git.

**Import flow** (verified no circular dependencies):

```
cmd/nebula_apply.go  →  nebula         (Load, Validate, BuildPlan, Apply)
                     →  nebula/worker  (NewWorkerGroup, Run)

nebula/worker        →  nebula         (PhaseSpec, State, WorkerResult, etc.)
                     →  agent, beads, dag, fabric, tycho (existing deps)

nebula               →  (does NOT import nebula/worker) ✓ NO CIRCULAR
```

**Import changes required**:

| File | Old Import | New Import |
|------|-----------|------------|
| `cmd/nebula_apply.go` | `internal/nebula` (for WorkerGroup, NewWorkerGroup, With*) | `internal/nebula` + `internal/nebula/worker` |
| `cmd/nebula_adapters.go` | `internal/nebula` (for PhaseRunnerResult, PhaseRunner) | `internal/nebula/worker` (for PhaseRunnerResult, PhaseRunner) |
| `cmd/nebula_status.go` | `internal/nebula` (for Metrics, LoadMetrics) | `internal/nebula/worker` (for Metrics, LoadMetrics) |
| `cmd/tui.go` | `internal/nebula` (for WorkerGroup, With*, Dashboard) | `internal/nebula` + `internal/nebula/worker` |
| `internal/tui/bridge.go` | N/A | No change (uses ui.UI interface, not worker directly) |
| `internal/tui/gater.go` | `internal/nebula` (for GateAction, Checkpoint) | `internal/nebula/worker` (for GateAction, Checkpoint, GatePrompter) |
| `internal/tui/msg.go` | `internal/nebula` (for PhaseStatus, Checkpoint) | `internal/nebula/worker` (for types that moved) |
| `internal/tui/gateprompt.go` | `internal/nebula` (for GateAction, Checkpoint) | `internal/nebula/worker` |
| `internal/tui/overlay.go` | `internal/nebula` (for WorkerResult) | `internal/nebula/worker` |
| `internal/tui/nebulaview.go` | No import from nebula currently | No change |

**Note**: Some types like `GateAction`, `Checkpoint`, `WorkerResult`, `Metrics`, `PhaseRunnerResult` move to the `worker` sub-package. Types that stay in `nebula` parent: `PhaseSpec`, `Nebula`, `State`, `Manifest`, `Plan`, `PhaseState`, `GateMode`, etc.

**Types that need careful placement**:

| Type | Stays in `nebula` | Moves to `worker` | Rationale |
|------|-------------------|-------------------|-----------|
| `PhaseSpec` | Yes | | Core data type used by parsing, validation, planning |
| `Nebula` | Yes | | Top-level manifest struct |
| `State`, `PhaseState` | Yes | | Persistence types used across concerns |
| `Manifest`, `Execution`, `Defaults` | Yes | | Configuration types |
| `WorkerResult` | Yes | | Return type consumed by cmd/ and nebula (plan logic) |
| `Plan`, `Action` | Yes | | Planning types |
| `WorkerGroup` | | Yes | Only created by cmd/, not used in parent |
| `Option`, `With*()` | | Yes | Builder pattern for WorkerGroup |
| `PhaseRunner`, `PhaseRunnerResult` | | Yes | Execution interface |
| `GateAction`, `Gater`, `GatePrompter` | | Yes | Runtime gating |
| `Checkpoint`, `FileChange` | | Yes | Runtime checkpoint during execution |
| `Scheduler`, `PhaseTracker` | | Yes | Runtime scheduling |
| `Metrics`, `PhaseMetrics`, `WaveMetrics` | | Yes | Runtime metrics |
| `Dashboard` | | Yes | Runtime visualization |
| `Watcher`, `HotReloader` | | Yes | Runtime file watching |
| `ProgressReporter`, `ProgressFunc` | | Yes | Runtime progress |

**Key design principle**: Types needed at parse/plan time stay in `nebula`. Types needed only at execution time move to `worker`.

#### 4b. Rename `internal/nebula/config.go` → `internal/nebula/resolve.go`

**Rationale**: Avoids confusion with `internal/config/config.go`. The file contains `ResolveExecution()` and `ResolveGate()` — it resolves execution parameters, not config loading. The name `resolve.go` better describes its purpose.

**Import changes**: None (same package, just rename).

---

### Tier 5: Naming Clarifications

These are pure renames for clarity, with no structural changes.

#### 5a. Rename `internal/nebula/checkpoint.go` → `internal/nebula/worker/phase_checkpoint.go`

This file moves as part of Tier 4a AND gets renamed. The rename avoids confusion with `internal/checkpoint/` (which handles serialization of loop state for persistence). The nebula checkpoint is specifically about capturing phase-level outcomes for gate review.

#### 5b. Rename worker files when moving to sub-package

When moving `worker_*.go` files to `internal/nebula/worker/`, strip the `worker_` prefix since the package name already provides context:

| Old Path | New Path |
|----------|----------|
| `nebula/worker_exec.go` | `nebula/worker/exec.go` |
| `nebula/worker_fabric.go` | `nebula/worker/fabric.go` |
| `nebula/worker_options.go` | `nebula/worker/options.go` |
| `nebula/worker_healing.go` | `nebula/worker/healing.go` |

---

## 3. Import Change Tracker

### Complete list of files requiring import path updates

After all changes, these files need import updates:

#### `cmd/` files:

| File | Change |
|------|--------|
| `cmd/nebula_apply.go` | Add `internal/nebula/worker` import; move `WorkerGroup`, `NewWorkerGroup`, `With*`, `Dashboard`, `NewDashboard` references to `worker.*` |
| `cmd/nebula_adapters.go` | Add `internal/nebula/worker` import; move `PhaseRunnerResult`, `PhaseRunner` references to `worker.*` |
| `cmd/nebula_status.go` | Add `internal/nebula/worker` import; move `Metrics`, `LoadMetrics`, `LoadMetricsWithHistory`, `HistorySummary` references to `worker.*` |
| `cmd/tui.go` | Add `internal/nebula/worker` import; move `WorkerGroup`, `NewWorkerGroup`, `With*`, `NewDashboard` references to `worker.*` |

#### `internal/tui/` files:

| File | Change |
|------|--------|
| `tui/gater.go` | Change `nebula.GateAction` → `worker.GateAction`, `nebula.GatePrompter` → `worker.GatePrompter`, `nebula.Checkpoint` → `worker.Checkpoint` |
| `tui/gateprompt.go` | Change `nebula.GateAction` → `worker.GateAction`, `nebula.Checkpoint` → `worker.Checkpoint` |
| `tui/overlay.go` | If it references `WorkerResult` directly (verify — it may use `nebula.WorkerResult` which stays in parent) |
| `tui/msg.go` | Update any types that moved (GateAction, Checkpoint, etc.) |
| `tui/nebulaview.go` | Likely no change (uses PhaseStatus which may stay in parent if it's used at parse time) |
| `tui/bridge.go` | Verify if it references any worker-specific types |

#### `internal/nebula/worker/` files (post-move):

| File | Change |
|------|--------|
| All `worker/*.go` files | Change `nebula.PhaseSpec` → keep importing parent `nebula` for shared types. Self-references change from `nebula.WorkerGroup` to just `WorkerGroup` (same package now). |

### Import additions summary

```
cmd/nebula_apply.go:    + "github.com/aaronsalm/quasar/internal/nebula/worker"
cmd/nebula_adapters.go: + "github.com/aaronsalm/quasar/internal/nebula/worker"
cmd/nebula_status.go:   + "github.com/aaronsalm/quasar/internal/nebula/worker"
cmd/tui.go:             + "github.com/aaronsalm/quasar/internal/nebula/worker"
tui/gater.go:           + "github.com/aaronsalm/quasar/internal/nebula/worker" (replaces some nebula refs)
tui/gateprompt.go:      + "github.com/aaronsalm/quasar/internal/nebula/worker" (replaces some nebula refs)
tui/msg.go:             + "github.com/aaronsalm/quasar/internal/nebula/worker" (replaces some nebula refs)
```

---

## 4. Files NOT Changed (and Why)

### `internal/tui/` — Stays flat (37 → 36 files after logo merge)

**Why no sub-packages?** BubbleTea's `Model` pattern requires all views to be composed in a single `AppModel` struct. `AppModel` embeds `NebulaView`, `LoopView`, `BoardView`, `DetailPanel`, etc. directly. If these were in sub-packages, every field access would need cross-package imports, and the tight message-passing pattern (all `Msg*` types shared across views) would require a separate types package. The complexity cost outweighs the organizational benefit.

The TUI files already follow a clear naming convention:
- `*view.go` — Major content views
- `*overlay.go` — Modal overlays
- `bridge.go`, `telemetry_bridge.go` — Integration bridges
- `msg.go` — All message types
- `model.go` — Root model
- `styles.go`, `layout.go` — Presentation

This naming provides sufficient navigability without sub-packages.

### `internal/loop/` — Stays flat (17 → 14 files after consolidation)

**Why?** After consolidating the finding micro-files and merging errors.go, the loop package is 14 files — a reasonable size for Go. The files cover: core loop, state, hooks, bead_hook, hail (3 files), findings, parse, prompts, report, lint, git, struggle. Each is a distinct concern at a manageable size (except loop.go which we split in Tier 3).

### `internal/dag/` — No changes (8 files)

**Why?** Well-organized graph algorithms package. Each file covers one algorithm or data structure (DAG core, analyzer, strategy, scoring, tracks, betweenness, pagerank, unionfind). All are cohesive and appropriately sized.

### `internal/fabric/` — No changes (11 files)

**Why?** While large, the files form a cohesive coordination substrate: core types, SQLite impl, 2 poller implementations, publisher, pushback handler, snapshot renderer, static scanner, contracts, discovery. Each has a clear single responsibility. The package doesn't warrant splitting because all files share the `Fabric` interface.

### `internal/agent/`, `internal/beads/`, `internal/claude/`, `internal/config/`, `internal/filter/`, `internal/snapshot/`, `internal/telemetry/`, `internal/neutron/`, `internal/tycho/`, `internal/ansi/`

**Why?** These are all appropriately sized (1-8 files each) with clear purposes. No changes needed.

### `internal/checkpoint/` — No changes (2 files)

**Why?** Clean separation: serialization logic (`checkpoint.go`) + event hook (`hook.go`). Imports `loop.CycleState` for conversion. No overlap with nebula's checkpoint (which captures phase-level outcomes, not loop state).

### `internal/arch_test/` — No changes (6 test files)

**Why?** Architecture enforcement tests. These intentionally span the entire codebase and belong at a top level.

---

## 5. Migration Strategy

### Execution Order

The changes should be applied in tier order to minimize breakage at each step:

**Step 1: Tier 1 — Delete dead code**
- Delete `internal/board/` directory
- Delete `internal/nebula/worker_board.go`
- Delete `internal/tui/architect_overlay.go`
- Run `go build ./...` and `go vet ./...` — should pass with no changes

**Step 2: Tier 2 — File consolidation**
- For each merge: copy contents into target file, delete source file, update test files
- Run `go build ./...` after each merge to verify
- Run `go test ./internal/loop/... ./internal/nebula/... ./internal/tui/...` after all merges

**Step 3: Tier 3 — File splitting**
- Split `loop/loop.go` into `loop_coder.go`, `loop_reviewer.go`, `loop_cache.go`
- Split `nebula/healing.go` into `healing.go` + `healing_remediate.go`
- Run `go build ./...` and `go test ./...` after each split

**Step 4: Tier 4 — Sub-package extraction (most complex)**
- Create `internal/nebula/worker/` directory
- Move files per the table in Tier 4a
- Update package declaration in all moved files: `package nebula` → `package worker`
- Update all internal references (remove `nebula.` prefix for same-package references, add `nebula.` prefix for parent-package type references)
- Update all external imports per the Import Change Tracker
- Rename files per Tier 5b
- Run `go build ./...` — fix any remaining import issues
- Run `go test ./...` — verify all tests pass

**Step 5: Tier 5 — Renames**
- Rename `nebula/config.go` → `nebula/resolve.go`
- These are already covered in Tier 4 moves

### Verification Checklist

After each step:
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` reports no issues
- [ ] `go test ./...` all tests pass
- [ ] `goimports` or `gofmt` applied to all modified files

---

## 6. Risk Assessment

### Low Risk (Tier 1 + 2)
- **Tier 1** (dead code deletion): Zero risk. No code references these files.
- **Tier 2** (file consolidation): Very low risk. Moving code between files in the same package doesn't affect compilation or behavior. The only risk is test file merge conflicts, which are easily resolved.

### Medium Risk (Tier 3)
- **File splitting**: Low-medium risk. Splitting files within the same package can't break imports, but care is needed to ensure all function references are in the right file and that test helpers are accessible from the right test file.

### Higher Risk (Tier 4)
- **Sub-package extraction**: This is the most complex change. The main risks are:
  1. **Missed import updates**: A reference to `nebula.WorkerGroup` that should now be `worker.WorkerGroup` could be missed. Mitigation: `go build ./...` will catch ALL such errors at compile time.
  2. **Type placement decisions**: Some types might need to be in both packages. If a type is used at parse time AND execution time, it must stay in the parent. We've already analyzed this (see the type placement table in Tier 4a).
  3. **Test file dependencies**: Test files that create WorkerGroups would need import updates. All test files in `nebula/` that test worker functionality should move to `nebula/worker/`.
  4. **Circular imports**: We've verified the dependency direction is one-way (`worker → nebula`, never `nebula → worker`), but any future development that adds a call from parent to child would break the build. This is actually a BENEFIT — the compiler enforces the boundary.

### Rollback Plan

Every change is committed separately (per step). If any step breaks the build, `git revert` the step and investigate. The tier ordering ensures earlier changes are independent of later ones, so a Tier 4 rollback doesn't affect Tier 1-3 changes.

---

## Appendix: Before & After Directory Comparison

### Before (19 packages, key counts)

```
internal/
  agent/          5 source files
  ansi/           1 source file
  arch_test/      6 test files
  beads/          2 source files
  board/          7 EMPTY source files + 6 test files  ← DELETE
  checkpoint/     2 source files
  claude/         3 source files
  config/         1 source file
  dag/            8 source files
  fabric/         11 source files
  filter/         3 source files
  loop/           17 source files
  nebula/         44 source files                      ← TOO MANY
  neutron/        2 source files
  snapshot/       3 source files
  telemetry/      1 source file
  tui/            37 source files
  tycho/          2 source files
  ui/             4 source files
```

### After (18 packages + 1 sub-package)

```
internal/
  agent/          5 source files                       (unchanged)
  ansi/           1 source file                        (unchanged)
  arch_test/      6 test files                         (unchanged)
  beads/          2 source files                       (unchanged)
  checkpoint/     2 source files                       (unchanged)
  claude/         3 source files                       (unchanged)
  config/         1 source file                        (unchanged)
  dag/            8 source files                       (unchanged)
  fabric/         11 source files                      (unchanged)
  filter/         3 source files                       (unchanged)
  loop/           14 source files                      (-3: consolidation + split)
  nebula/         17 source files                      (-27: consolidation + extraction)
    worker/       19 source files                      (NEW sub-package)
  neutron/        2 source files                       (unchanged)
  snapshot/       3 source files                       (unchanged)
  telemetry/      1 source file                        (unchanged)
  tui/            36 source files                      (-1: logo merge + empty delete)
  tycho/          2 source files                       (unchanged)
  ui/             4 source files                       (unchanged)
```

**Key improvements**:
- `board/` deleted (13 files removed)
- `nebula/` goes from 44 → 17 files (manageable, focused on types/parsing/validation/planning/generation)
- New `nebula/worker/` has 19 files (all execution-time orchestration)
- `loop/` goes from 17 → 14 files (micro-files consolidated)
- Total non-test files: 148 → 138 (-10)
- Total files including tests: ~280 → ~255 (-25)
- Dead/empty files eliminated: 10
