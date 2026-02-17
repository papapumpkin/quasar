+++
id = "ui-printer"
title = "Test UI Printer output methods"
type = "task"
priority = 2
+++

## Goal

Increase `internal/ui` coverage from 31.9% to ~70% by testing the Printer's output methods.

## Current State

Almost all `Printer` methods are at 0% coverage:

- `Banner`, `Prompt`, `CycleStart`, `AgentStart`, `AgentDone`
- `IssuesFound`, `Approved`, `MaxCyclesReached`, `BudgetExceeded`
- `Error`, `Info`, `AgentOutput`, `BeadUpdate`, `RefactorApplied`
- `TaskStarted`, `TaskComplete`
- `ShowHelp`, `ShowStatus`
- `NebulaValidateResult`, `NebulaPlan`, `NebulaApplyDone`, `NebulaWorkerResults`
- `ReviewReport`, `NebulaShow`
- `NebulaProgressBarDone`, partial `NebulaStatus` (53%)
- `ANSICursorUp` (0%)

Already tested: `CycleSummary` (100%), `New` (100%), `NebulaProgressBar` (100%), `NebulaProgressBarLine` (100%), formatters.

## Approach

1. `Printer` writes to `os.Stderr`. Create a `Printer` with `New()`, redirect stderr to a buffer (or use `Printer` with an `io.Writer` if the constructor supports it — check the implementation).
2. Call each method with representative inputs.
3. Assert output contains expected substrings (use `strings.Contains` per CLAUDE.md conventions).
4. Group tests by category: lifecycle methods, nebula methods, status methods.

## Files

- `internal/ui/ui_test.go` — extend with new test functions
- `internal/ui/ui.go` — read for understanding

## Scope

- `internal/ui/` only
