+++
id = "inner-fix-loop"
title = "Inner coder-fix loop for filter failures inside the outer coder-reviewer cycle"
type = "feature"
priority = 1
depends_on = ["error-parser", "fix-prompt-builder"]
labels = ["quasar", "filter", "cost-optimization"]
scope = ["internal/loop/loop.go", "internal/loop/loop_test.go", "internal/loop/state.go"]
+++

## Problem

Today when a filter check fails, `runFilterChecks` in `internal/loop/loop.go` (lines 281-316) creates a synthetic finding, marks the phase as `PhaseResolvingIssues`, and the outer `runLoop` continues to the next coder-reviewer cycle. This means a single build error costs a full coder invocation (re-reads task, all findings, all context) plus burns one of the limited `MaxCycles`.

The existing `runLintFixLoop` (lines 209-275) already implements the right pattern: a tight inner loop that feeds lint output to the coder and re-runs the lint check, up to `maxLintRetries` times. But it is lint-specific and does not work for build, vet, or test failures.

We need to generalize this into a `runFilterFixLoop` that:
1. Parses the failing check's output into structured errors
2. Sends a hyper-focused fix prompt to the coder
3. Re-runs ONLY the failing check (not the whole chain)
4. Repeats until the check passes or the retry limit is reached
5. Only then falls through to the existing outer-cycle bounce behavior

## Solution

### New State Fields

Add to `CycleState` in `internal/loop/state.go`:

```go
type CycleState struct {
    // ... existing fields ...
    FilterFixAttempts int  // number of inner fix attempts in the current cycle
    FilterFixCostUSD  float64 // cost accumulated during filter fix attempts this cycle
}
```

### New Configuration

Add to `Loop` struct in `internal/loop/loop.go`:

```go
type Loop struct {
    // ... existing fields ...
    MaxFilterFixes int // Max inner fix attempts per filter failure. 0 uses DefaultMaxFilterFixes.
}
```

And a constant + helper:

```go
const DefaultMaxFilterFixes = 3

func (l *Loop) maxFilterFixes() int {
    if l.MaxFilterFixes > 0 {
        return l.MaxFilterFixes
    }
    return DefaultMaxFilterFixes
}
```

### Core Method: `runFilterFixLoop`

```go
// runFilterFixLoop attempts to fix a failing filter check via a fast inner loop.
// It parses the error output, sends a focused fix prompt to the coder, re-runs
// only the failing check, and repeats up to maxFilterFixes times. Returns true
// if the check ultimately passes, false if retries are exhausted.
//
// Claims check failures are never short-circuited — they require coordination,
// not code fixes, so they always fall through to the outer cycle.
func (l *Loop) runFilterFixLoop(ctx context.Context, state *CycleState, checkName string, checkOutput string) (fixed bool, err error)
```

Implementation outline:

1. **Guard**: If `checkName == "claims"`, return `(false, nil)` immediately. Claims failures need human/fabric coordination, not automated fixes.

2. **Parse**: Call `filter.ParseCheckOutput(CheckResult{Name: checkName, Output: checkOutput})` to get structured errors.

3. **Inner loop** (up to `maxFilterFixes` attempts):
   a. Build the fix prompt via `l.buildFilterFixPrompt(state, parsed)`.
   b. Invoke the coder with `filterFixBudget()` and a restricted tool set (Read, Edit, Write, Glob only — no Bash to prevent the coder from running arbitrary commands during a fix pass).
   c. Update `state.TotalCostUSD` and `state.FilterFixCostUSD`.
   d. Check budget via `l.checkBudget()`.
   e. Commit the fix if `l.Git != nil` (message: `summary + " (filter fix)"`).
   f. Re-run ONLY the failing check by finding it in `l.Filter` and executing its `Fn` directly.
   g. If the check passes, return `(true, nil)`.
   h. If it fails again, update `checkOutput` and `parsed`, increment `state.FilterFixAttempts`, and continue.

4. **Exhaust**: After the loop, return `(false, nil)` — the caller falls through to the existing outer-cycle bounce.

### Re-running a Single Check

The `Chain` struct holds `[]Check` with `Name` and `Fn` fields. To re-run a single check without running the whole chain, add a method to `Chain`:

```go
// RunCheck executes a single named check from the chain.
// Returns the CheckResult, or an error if the check name is not found.
func (c *Chain) RunCheck(ctx context.Context, workDir string, name string) (*CheckResult, error)
```

Since `Loop.Filter` is typed as `filter.Filter` (the interface), we need to either:
- Add `RunCheck` to the `Filter` interface (breaking change — avoid), or
- Type-assert `l.Filter` to `*filter.Chain` when entering the fix loop, or
- Create a new `SingleCheckRunner` interface consumed where needed.

The cleanest approach per project conventions (interfaces at consumption site):

```go
// SingleCheckRunner can execute a single named check. Defined in the loop
// package where consumed, satisfied by *filter.Chain.
type SingleCheckRunner interface {
    RunCheck(ctx context.Context, workDir string, name string) (*filter.CheckResult, error)
}
```

Then in `runFilterFixLoop`:
```go
runner, ok := l.Filter.(SingleCheckRunner)
if !ok {
    // Filter doesn't support single-check re-runs; fall through to outer cycle.
    return false, nil
}
```

### Integration into `runLoop`

Replace the current filter failure handling block (lines 133-145 of `loop.go`) with:

```go
if l.Filter != nil {
    failed, err := l.runFilterChecks(ctx, state)
    if err != nil {
        return nil, err
    }
    if failed {
        // Attempt fast inner fix loop before burning a full outer cycle.
        fixed, err := l.runFilterFixLoop(ctx, state, state.FilterCheckName, state.FilterOutput)
        if err != nil {
            return nil, err
        }
        if !fixed {
            // Inner loop exhausted — fall through to outer cycle bounce.
            l.sealCycleSHA(state)
            l.drainRefactor(state)
            l.emit(ctx, Event{Kind: EventCycleStart, BeadID: beadID, Cycle: cycle})
            continue
        }
        // Fixed! Clear filter state and proceed to reviewer.
        state.FilterOutput = ""
        state.FilterCheckName = ""
        state.FilterFixAttempts = 0
    }
}
```

### Interaction with `runLintFixLoop`

The existing `runLintFixLoop` runs BEFORE the filter chain. Lint fixing via `runLintFixLoop` handles the `Linter` interface (which is separate from `Filter`). The new `runFilterFixLoop` handles failures from the filter chain, which includes its own lint check via `lintCheck`.

To avoid double-linting:
- If the filter's lint check fails AND `runLintFixLoop` already ran, the inner fix loop picks up where lint fixing left off.
- If `l.Linter` is nil but the filter chain includes lint, the inner fix loop handles it.
- No special coordination needed — the filter chain runs after lint fixing, so any remaining lint issues are genuinely new or unfixed.

## Files

- `internal/loop/loop.go` — Add `MaxFilterFixes` field, `maxFilterFixes()`, `runFilterFixLoop()`, `SingleCheckRunner` interface; modify filter failure handling in `runLoop`
- `internal/loop/state.go` — Add `FilterFixAttempts` and `FilterFixCostUSD` fields to `CycleState`
- `internal/filter/chain.go` — Add `RunCheck` method to `Chain`
- `internal/loop/loop_test.go` — Table-driven tests for `runFilterFixLoop` with mock filter and invoker
- `internal/filter/chain_test.go` — Test `RunCheck` method

## Acceptance Criteria

- [ ] `runFilterFixLoop` parses errors, invokes coder with focused prompt, and re-runs only the failing check
- [ ] Claims check failures are never short-circuited (guard at top of method)
- [ ] Inner loop respects `MaxFilterFixes` / `DefaultMaxFilterFixes` limit
- [ ] Budget is checked after each fix attempt via `l.checkBudget()`
- [ ] Git commits are created after each fix attempt with `" (filter fix)"` suffix
- [ ] `SingleCheckRunner` interface defined in loop package, satisfied by `*filter.Chain`
- [ ] `Chain.RunCheck` returns the result for a single named check
- [ ] `runLoop` integration: fix loop runs before outer-cycle bounce, fixed results proceed to reviewer
- [ ] `FilterFixAttempts` and `FilterFixCostUSD` tracked in `CycleState`
- [ ] Coder fix invocations use restricted tool set (Read, Edit, Write, Glob — no Bash)
- [ ] All existing tests pass: `go test ./internal/loop/... ./internal/filter/...`
- [ ] New tests cover: successful fix, exhausted retries, claims guard, budget exceeded, nil filter
