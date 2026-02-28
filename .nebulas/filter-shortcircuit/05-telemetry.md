+++
id = "telemetry"
title = "Telemetry events and UI output for filter short-circuit"
type = "feature"
priority = 2
depends_on = ["inner-fix-loop"]
labels = ["quasar", "filter", "cost-optimization"]
scope = ["internal/telemetry/telemetry.go", "internal/loop/hooks.go", "internal/loop/loop.go"]
allow_scope_overlap = true
+++

## Problem

The inner fix loop introduced in the `inner-fix-loop` phase operates silently. There is no telemetry to track:
- How often filter short-circuits fire vs. falling through to full outer cycles
- Cost savings from focused fix invocations vs. full coder re-runs
- Which check types fail most often and whether fixes succeed on first attempt
- The wall-clock time saved by avoiding reviewer invocations

Without this data, we cannot measure whether the feature is delivering its cost-optimization promise, nor can we tune `MaxFilterFixes` or `filterFixBudget` based on real-world performance.

The UI also needs updates: operators watching the TUI or stderr output should see filter fix attempts in real time, similar to how `runLintFixLoop` prints "lint issues found (attempt 1/3), sending back to coder".

## Solution

### New Telemetry Event Kinds

Add to `internal/telemetry/telemetry.go`:

```go
const (
    // ... existing kinds ...
    KindFilterFixAttempt = "filter_fix_attempt"  // emitted per inner fix attempt
    KindFilterFixResult  = "filter_fix_result"   // emitted when inner fix loop concludes
)
```

### New Loop Event Kinds

Add to the `EventKind` enum in `internal/loop/hooks.go`:

```go
const (
    // ... existing kinds ...
    // EventFilterFixAttempt is emitted at each inner fix loop iteration.
    EventFilterFixAttempt EventKind = iota // (add after the last existing kind)
    // EventFilterFixResult is emitted when the inner fix loop concludes,
    // whether by success or retry exhaustion.
    EventFilterFixResult
)
```

Update `Event` struct to carry filter-fix-specific data:

```go
type Event struct {
    // ... existing fields ...
    FilterFix *FilterFixData // populated for EventFilterFixAttempt and EventFilterFixResult
}

// FilterFixData carries metadata about a filter fix attempt or result.
type FilterFixData struct {
    CheckName    string  // which filter check failed ("build", "vet", "lint", "test")
    Attempt      int     // 1-based attempt number
    MaxAttempts  int     // maximum attempts configured
    Fixed        bool    // true if the check passed after this attempt (or overall for result)
    CostUSD      float64 // cost of this fix invocation (attempt) or total fix cost (result)
    ErrorCount   int     // number of structured errors parsed from the check output
    DurationMs   int64   // wall-clock time for this fix attempt
}
```

### Emit Points in `runFilterFixLoop`

At each iteration of the inner loop:

```go
l.emit(ctx, Event{
    Kind:   EventFilterFixAttempt,
    BeadID: state.TaskBeadID,
    Cycle:  state.Cycle,
    FilterFix: &FilterFixData{
        CheckName:   checkName,
        Attempt:     attempt + 1,
        MaxAttempts:  l.maxFilterFixes(),
        Fixed:       checkPassed,
        CostUSD:     fixResult.CostUSD,
        ErrorCount:  len(parsed.Errors),
        DurationMs:  fixResult.DurationMs,
    },
})
```

At the conclusion of the inner loop (success or exhaustion):

```go
l.emit(ctx, Event{
    Kind:   EventFilterFixResult,
    BeadID: state.TaskBeadID,
    Cycle:  state.Cycle,
    FilterFix: &FilterFixData{
        CheckName:   checkName,
        Attempt:     state.FilterFixAttempts,
        MaxAttempts:  l.maxFilterFixes(),
        Fixed:       fixed,
        CostUSD:     state.FilterFixCostUSD,
        ErrorCount:  len(parsed.Errors),
    },
})
```

### UI Output

Add calls to `l.UI` in `runFilterFixLoop` to surface progress to the operator. Follow the pattern from `runLintFixLoop`:

```go
// At start of inner loop:
l.UI.Info(fmt.Sprintf("filter check %q failed with %d errors, attempting targeted fix (attempt %d/%d)",
    checkName, len(parsed.Errors), attempt+1, l.maxFilterFixes()))

// After successful fix:
l.UI.Info(fmt.Sprintf("filter check %q fixed on attempt %d", checkName, attempt+1))

// After exhausting retries:
l.UI.Info(fmt.Sprintf("filter check %q not fixed after %d attempts, falling back to outer cycle",
    checkName, l.maxFilterFixes()))

// Cost logging per attempt:
l.UI.AgentDone("coder", fixResult.CostUSD, fixResult.DurationMs)
```

### Telemetry Hook Integration

The existing `TelemetryHook` (if present in `l.Hooks`) receives events via `OnEvent`. The hook should map the new event kinds to JSONL telemetry events:

- `EventFilterFixAttempt` -> `KindFilterFixAttempt` with `FilterFixData` as `Data`
- `EventFilterFixResult` -> `KindFilterFixResult` with `FilterFixData` as `Data`

This follows the existing pattern where `EventAgentDone` maps to `KindAgentDone`.

### CycleSummary Extension

The `ui.CycleSummaryData` struct should be extended with filter fix information so the TUI can display it:

```go
type CycleSummaryData struct {
    // ... existing fields ...
    FilterFixAttempts int     // number of inner fix attempts this cycle (0 = no filter failures)
    FilterFixCostUSD  float64 // cost spent on filter fixes this cycle
    FilterFixSuccess  bool    // true if filter was fixed via inner loop
}
```

Populate this in `emitCycleSummary` from `state.FilterFixAttempts` and `state.FilterFixCostUSD`.

## Files

- `internal/telemetry/telemetry.go` — Add `KindFilterFixAttempt` and `KindFilterFixResult` constants
- `internal/loop/hooks.go` — Add `EventFilterFixAttempt`, `EventFilterFixResult` to `EventKind` enum; add `FilterFixData` struct and `FilterFix` field to `Event`
- `internal/loop/loop.go` — Emit `EventFilterFixAttempt` and `EventFilterFixResult` in `runFilterFixLoop`; add UI calls for progress output
- `internal/ui/ui.go` — Extend `CycleSummaryData` with filter fix fields (if struct is defined here)

## Acceptance Criteria

- [ ] `KindFilterFixAttempt` and `KindFilterFixResult` telemetry constants added
- [ ] `EventFilterFixAttempt` and `EventFilterFixResult` added to `EventKind` enum
- [ ] `FilterFixData` struct captures check name, attempt, max attempts, fixed status, cost, error count, duration
- [ ] `runFilterFixLoop` emits `EventFilterFixAttempt` per iteration and `EventFilterFixResult` at conclusion
- [ ] UI shows filter fix progress messages via `l.UI.Info()` following the `runLintFixLoop` pattern
- [ ] `CycleSummaryData` extended with `FilterFixAttempts`, `FilterFixCostUSD`, `FilterFixSuccess`
- [ ] Telemetry hook maps new events to JSONL with `FilterFixData` as payload
- [ ] All existing tests pass: `go test ./internal/loop/... ./internal/telemetry/...`
- [ ] No regressions in TUI or stderr printer output
