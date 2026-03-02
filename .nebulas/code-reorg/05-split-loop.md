+++
id = "split-loop"
title = "Split internal/loop/loop.go (~1000 lines) into focused files"
type = "task"
priority = 2
depends_on = ["consolidate-loop"]
scope = ["internal/loop/"]
+++

## Problem

`internal/loop/loop.go` is ~1000 lines containing the entire coder-reviewer cycle engine in a single file. It mixes core orchestration, coder execution, reviewer execution, filter-fix loops, prompt caching, finding creation, hail extraction, and cycle commit sealing. Finding a specific method requires scrolling through unrelated code.

## Solution

Split `loop.go` into 4 focused files. All remain in `package loop` — no import changes needed.

### File 1: `loop.go` (stays, ~400 lines)
Core orchestration and lifecycle:
- `Loop` struct definition and all its fields
- `RunTask()`, `RunExistingTask()`, `RunFromCheckpoint()` — entry points
- The main cycle `for` loop body
- `checkBudget()`, `perAgentBudget()` — budget tracking
- `emit()` — event emission helper
- `handleApproval()` — approval handling
- `sealCycleSHA()`, `finalCommitSHA()` — git commit sealing
- `GenerateCheckpoint()` — checkpoint generation

### File 2: `loop_coder.go` (new, ~250 lines)
Coder agent execution and fix loops:
- `runCoderPhase()` — invoke coder agent
- `runLintFixLoop()` — lint → fix → retry loop
- `runFilterChecks()` — run deterministic pre-reviewer filters
- `runFilterFixLoop()` — filter failure → fix → retry loop
- `drainRefactor()` — handle mid-run task description changes

### File 3: `loop_reviewer.go` (new, ~200 lines)
Reviewer agent execution and post-review processing:
- `runReviewerPhase()` — invoke reviewer agent
- `emitCycleSummary()` — emit structured cycle metadata
- `extractAndPostHails()` — extract escalation requests from reviewer output
- `postMaxCyclesHail()` — create hail when max cycles reached
- `createFindingBeads()` — create child beads for findings
- `emitBeadUpdate()` — emit bead hierarchy update

### File 4: `loop_cache.go` (new, ~100 lines)
Prompt caching optimization:
- `cacheSystemPrompts()` — pre-compute stable coder/reviewer system prompts once per task
- `trackCacheMetrics()` — record cache hit/miss statistics

### Approach

Read `loop.go` fully, identify each function's boundaries, and move them to the appropriate new file. Keep the `Loop` struct and entry-point methods in the original `loop.go`. Methods that are receivers on `*Loop` can freely move between files in the same package.

For each new file, include only the imports needed by the functions in that file. Do NOT duplicate the entire import block.

## Files

- `internal/loop/loop.go` — trim to core orchestration (~400 lines)
- `internal/loop/loop_coder.go` — create (coder phase + fix loops)
- `internal/loop/loop_reviewer.go` — create (reviewer phase + post-processing)
- `internal/loop/loop_cache.go` — create (prompt caching)

## Acceptance Criteria

- [ ] `loop.go` is under 500 lines
- [ ] Each new file has a clear single responsibility
- [ ] No functions are duplicated across files
- [ ] All methods remain accessible (same package)
- [ ] `go build ./internal/loop/...` succeeds
- [ ] `go test ./internal/loop/...` passes
- [ ] `go vet ./internal/loop/...` passes
