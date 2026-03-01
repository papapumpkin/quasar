+++
id = "filter-caching"
title = "Skip passing checks on re-run after inner fix loop"
type = "feature"
priority = 2
depends_on = ["inner-fix-loop"]
labels = ["quasar", "filter", "cost-optimization"]
scope = ["internal/filter/chain.go", "internal/filter/filter.go", "internal/filter/chain_test.go"]
+++

## Problem

After the inner fix loop resolves a failing check (e.g., `build`), the filter chain needs to verify that ALL checks still pass before handing off to the reviewer. Today, `Chain.Run` executes every check sequentially from the start: build, vet, lint, test, claims. This means a successful fix of a build error triggers a full re-run of build (again), vet, lint, test, and claims — even though vet/lint/test/claims were never the problem.

For most filter failures, re-running the full chain is cheap (seconds). But `go test ./...` can take 30+ seconds on larger codebases, and lint can be slow too. When the inner fix loop runs 2-3 attempts, each triggering a full chain validation, the cumulative overhead adds up.

More importantly, this is about correctness: the inner fix loop uses `RunCheck` to re-run only the failing check after each fix attempt. But after the inner loop succeeds, the outer code should verify the full chain passes (the fix might have broken something else). The question is whether we can skip checks that passed before the failure.

## Solution

Add a `RunFrom` method to `Chain` that starts execution from a named check, skipping all checks before it. This lets the outer code re-run the chain from the point of failure onward, rather than from scratch.

### New Method on `Chain`

```go
// RunFrom executes the chain starting from the named check, skipping all
// checks before it. This is useful after an inner fix loop resolves a
// failure — checks before the failure point already passed and don't need
// re-running (the fix was scoped to the failing check's domain).
//
// If the named check is not found, RunFrom behaves like Run (executes all).
// Stops on first failure, same as Run.
func (c *Chain) RunFrom(ctx context.Context, workDir string, startFrom string) (*Result, error)
```

Implementation: find the index of `startFrom` in `c.Checks`, then iterate from that index forward using the same logic as `Run`. Checks before the start point get synthetic `CheckResult{Passed: true}` entries so the `Result.Checks` slice always has the full picture.

### Prefilling Passed Results

The `Result.Checks` slice should still contain entries for skipped checks so callers don't need to handle partial results. Skipped checks get:

```go
CheckResult{
    Name:    check.Name,
    Passed:  true,
    Output:  "", // no output — was cached/skipped
    Elapsed: 0,  // zero duration signals "skipped"
}
```

A zero `Elapsed` with `Passed: true` is a clear signal to telemetry and UI that the check was skipped, not run.

### Integration Point

In `runLoop` (after the inner fix loop succeeds), instead of running `l.Filter.Run(ctx, l.WorkDir)` again, use:

```go
if chain, ok := l.Filter.(*filter.Chain); ok {
    result, err = chain.RunFrom(ctx, l.WorkDir, state.FilterCheckName)
} else {
    result, err = l.Filter.Run(ctx, l.WorkDir)
}
```

This is a pure optimization — if the filter is not a `*Chain` (custom implementation), fall back to full re-run. The `RunFrom` path saves time by skipping build (which already passed) and jumping straight to the check that failed.

### Safety Consideration

Skipping earlier checks is safe because:
1. The coder fix was scoped to the failing check's errors (the focused prompt forbids unrelated changes).
2. Build errors can't be introduced by fixing vet/lint/test issues.
3. The check ordering (build -> vet -> lint -> test -> claims) is a dependency chain: if build passed before and the fix only touched code to resolve a vet issue, build will still pass.

The one edge case is if the coder's fix introduces a NEW build error while fixing a test failure. This is handled by re-running from the failure point: the build check is before test in the chain, so if we re-run from `test`, we skip `build`. To handle this edge case conservatively, `RunFrom` should re-run from ONE check BEFORE the named check when the named check is not the first in the chain:

```go
// startIdx is max(0, failIdx-1) to catch regressions from fixes
```

This adds one extra cheap check (e.g., re-running vet when lint failed) while still skipping the majority of the chain.

## Files

- `internal/filter/chain.go` — Add `RunFrom` method to `Chain`
- `internal/filter/chain_test.go` — Table-driven tests for `RunFrom` with various start points
- `internal/loop/loop.go` — Use `RunFrom` after successful inner fix loop

## Acceptance Criteria

- [ ] `Chain.RunFrom` starts execution from the named check (or one before for regression safety)
- [ ] Skipped checks have synthetic `CheckResult{Passed: true, Elapsed: 0}` entries in `Result.Checks`
- [ ] Unknown `startFrom` name falls back to running all checks (same as `Run`)
- [ ] Stops on first failure, same as `Run`
- [ ] `runLoop` uses `RunFrom` after successful inner fix loop, falls back to `Run` for non-Chain filters
- [ ] Table-driven tests cover: start from first check, start from middle, start from last, unknown name, regression safety (start one before)
- [ ] `go build ./...` and `go test ./internal/filter/...` pass
