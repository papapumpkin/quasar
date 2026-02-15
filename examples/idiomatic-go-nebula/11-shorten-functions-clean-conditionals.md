+++
id = "shorten-funcs-clean-conds"
title = "Shorten remaining long functions and simplify nested conditional logic"
type = "task"
priority = 2
depends_on = ["extract-ui-interface"]
+++

## Problem

Several functions outside `runLoop` and `WorkerGroup.Run` (covered by tasks 03/04) exceed 20 lines or contain deeply nested conditional chains that hurt readability.

### Long functions

- **`cmd/run.go: runRun`** (142 lines) — Mixes config flag overrides, prompt file loading, dependency validation, working directory resolution, signal setup, auto mode, and REPL loop all in one function.
- **`internal/nebula/apply.go: Apply`** (103 lines) — The `ActionCreate` and `ActionRetry` cases share near-identical code (create bead → set state → save state). The entire switch body is verbose because each case inlines all its logic.

### Nested / verbose conditionals

- **`internal/loop/loop.go: ParseReviewFindings`** (47 lines, 4 nesting levels) — An outer `for` loop contains an `if "ISSUE:"` check, inside which is another `for` loop with an `if/else if` chain for `SEVERITY:` / `DESCRIPTION:`, and inside DESCRIPTION is yet another `for` loop collecting continuation lines.
- **`internal/loop/report.go: ParseReviewReport`** (41 lines, `if/else if` x4) — Four branches each doing `strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "KEY:")))`. This repeated pattern would be cleaner with a `switch` on the field prefix or a map-based lookup.

## Solution

### `cmd/run.go`
Extract helpers from `runRun`:
- `applyFlagOverrides(cmd, cfg)` — reads flag values, applies to config
- `loadPrompts(cmd, cfg) (coder, reviewer string, err error)` — resolves prompt sources
- `buildLoop(cfg, coderPrompt, reviewerPrompt) (*loop.Loop, error)` — validates deps, constructs the Loop
- Keep `runRun` as a ~30-line orchestrator: load config → apply overrides → load prompts → build loop → setup signal → branch auto/REPL

### `internal/nebula/apply.go`
- Extract `applyCreateBead(client, task, state, dir) error` — shared by both `ActionCreate` and `ActionRetry`
- Extract `applyCloseBead(client, state, action, dir) error`
- `Apply` becomes a thin loop with a switch that delegates to helpers

### `internal/loop/loop.go: ParseReviewFindings`
- Extract `parseIssueBlock(lines []string, start int) (ReviewFinding, int)` — parses one ISSUE: block starting at index, returns the finding and the new index. Handles SEVERITY, DESCRIPTION, and continuation lines.
- `ParseReviewFindings` becomes: split lines → scan for "ISSUE:" → call `parseIssueBlock` → append

### `internal/loop/report.go: ParseReviewReport`
Replace the `if/else if` chain with a `switch` on the field prefix:
```go
switch {
case strings.HasPrefix(line, "SATISFACTION:"):
    report.Satisfaction = parseField(line, "SATISFACTION:")
case strings.HasPrefix(line, "RISK:"):
    report.Risk = parseField(line, "RISK:")
// ...
}
```
Extract `parseField(line, prefix string) string` that does the `ToLower(TrimSpace(TrimPrefix(...)))` once.

## Files to Modify

- `cmd/run.go` — Extract `applyFlagOverrides`, `loadPrompts`, `buildLoop` from `runRun`
- `internal/nebula/apply.go` — Extract `applyCreateBead`, `applyCloseBead` from `Apply`
- `internal/loop/loop.go` — Extract `parseIssueBlock` from `ParseReviewFindings`
- `internal/loop/report.go` — Add `parseField` helper, convert `if/else if` to `switch`

## Acceptance Criteria

- [ ] `runRun` is under 40 lines
- [ ] `Apply` is under 40 lines
- [ ] `ParseReviewFindings` is under 25 lines with no more than 2 nesting levels
- [ ] `ParseReviewReport` uses `switch` instead of `if/else if` chain, with a shared `parseField` helper
- [ ] Each extracted helper is under 25 lines
- [ ] No behavior changes — all existing tests pass
- [ ] `go test ./...` passes