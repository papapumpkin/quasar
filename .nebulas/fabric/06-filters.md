+++
id = "filters"
title = "Pre-reviewer deterministic filters"
type = "feature"
priority = 2
depends_on = ["fabric-rename"]
scope = ["internal/filter/**"]
+++

## Problem

The coder-reviewer loop currently sends every coder output to the reviewer agent, even when the code doesn't compile or fails existing tests. This wastes full reviewer inference on issues that a compiler catches in 200ms. The design calls for deterministic pre-review filters that bounce failures back to the coder before the reviewer ever sees them.

## Solution

Create `internal/filter` with a `Filter` type that runs a chain of deterministic checks after the coder finishes and before the reviewer starts.

### Filter interface

```go
// Filter runs deterministic checks on coder output.
// Returns nil if all checks pass, or an error describing the first failure.
type Filter interface {
    Run(ctx context.Context, workDir string) (*Result, error)
}

// Result contains the outcome of filter execution.
type Result struct {
    Passed  bool
    Checks  []CheckResult
}

// CheckResult is the outcome of a single check.
type CheckResult struct {
    Name    string // "build", "vet", "lint", "test", "claims"
    Passed  bool
    Output  string // stdout+stderr on failure
    Elapsed time.Duration
}
```

### Built-in checks

Run in this order, stop on first failure:

1. **Build**: `go build ./...` — must compile
2. **Vet**: `go vet ./...` — static analysis
3. **Lint**: `golangci-lint run` (if available on PATH, skip if not)
4. **Test**: `go test ./...` — existing tests must pass
5. **Claims**: verify every file modified since the coder started was properly claimed on the fabric (requires `Fabric` reference, skip if Fabric is nil)

Each check runs via `exec.CommandContext(ctx, ...)` for cancellation support.

### Chain implementation

```go
// Chain runs filters sequentially, returning on first failure.
type Chain struct {
    Checks []Check
}

type Check struct {
    Name string
    Fn   func(ctx context.Context, workDir string) (string, error)
}

// DefaultChain returns the standard pre-reviewer filter chain.
func DefaultChain(fabric Fabric) *Chain
```

### Integration point

The filter chain is called in `loop.Loop` between the coder phase and the reviewer phase. If the filter fails:
- The coder gets the filter output as feedback (same as reviewer issues)
- The reviewer is NOT invoked
- The cycle counter increments
- The loop continues with the coder receiving the filter error

This is a change to `internal/loop/loop.go` — add an optional `Filter` field:
```go
type Loop struct {
    // ... existing fields
    Filter filter.Filter // nil = skip filtering, go straight to reviewer
}
```

The filter result is surfaced through the existing `MsgIssuesFound` / `MsgPhaseIssuesFound` message path with a note that these are filter failures, not reviewer feedback.

## Files

- `internal/filter/filter.go` — `Filter` interface, `Result`, `CheckResult` types
- `internal/filter/chain.go` — `Chain` implementation with build/vet/lint/test/claims checks
- `internal/filter/chain_test.go` — Tests using temp directories with known-bad Go code
- `internal/loop/loop.go` — Add optional `Filter` field, invoke between coder and reviewer

## Acceptance Criteria

- [ ] `DefaultChain` runs build, vet, lint (if available), test, claims (if fabric present)
- [ ] Chain stops on first failure and returns the failing check's output
- [ ] Checks run with context for cancellation support
- [ ] Claims check validates modified files against fabric (skipped if fabric is nil)
- [ ] `Loop.Filter` is optional — nil means reviewer is invoked directly (backward compatible)
- [ ] Filter failures bounce to coder with the check output
- [ ] `go test ./internal/filter/...` passes
- [ ] `go vet ./...` clean
