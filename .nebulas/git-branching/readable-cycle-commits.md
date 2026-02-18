+++
id = "readable-cycle-commits"
title = "Human-readable cycle commit messages with summary"
type = "feature"
priority = 2
depends_on = ["nebula-branch-lifecycle"]
+++

## Problem

Cycle commit messages are currently `quasar: <beadID> cycle-<N>`, e.g.:

```
quasar: quasar-kf8 cycle-2
```

This is machine-trackable but not human-readable. There's no indication of *what* the cycle accomplished. The user wants the format:

```
quasar-kf8/cycle-2: Fix status bar wrapping
```

This preserves traceability (bead ID + cycle number) while adding a human-readable summary.

## Solution

### 1. Update `CycleCommitter` interface

Change `CommitCycle` to accept a `summary` parameter:

```go
// CommitCycle stages all changes and creates a commit for the given cycle.
// The summary is a short human-readable description included in the commit message.
// Returns the HEAD SHA after the commit.
CommitCycle(ctx context.Context, label string, cycle int, summary string) (sha string, err error)
```

### 2. Update commit message format

In `gitCycleCommitter.CommitCycle`, change the format from:

```go
msg := fmt.Sprintf("quasar: %s cycle-%d", label, cycle)
```

To:

```go
msg := fmt.Sprintf("%s/cycle-%d: %s", label, cycle, summary)
```

This produces: `quasar-kf8/cycle-2: Fix status bar wrapping`

### 3. Thread summary through the Loop

Add a `CommitSummary` field to the `Loop` struct:

```go
type Loop struct {
    // ...
    CommitSummary string // Short label for cycle commit messages. If empty, derived from task title.
}
```

In `runCoderPhase` (where `CommitCycle` is called), compute the summary:

```go
summary := l.CommitSummary
if summary == "" {
    summary = firstLine(state.TaskTitle, 72)
}
sha, err := l.Git.CommitCycle(ctx, state.TaskBeadID, state.Cycle, summary)
```

Add a helper `firstLine(s string, maxLen int) string` that extracts the first line of text and truncates to maxLen characters.

### 4. Set CommitSummary in nebula adapters

In `cmd/nebula_adapters.go`, the adapters need the phase title to set `CommitSummary`. Currently `RunExistingPhase` doesn't receive the title separately from the description.

**Option A (preferred)**: Add a `title` field to the adapter that's set per-phase. Since `executePhase` in `worker_exec.go` has access to `phase.Title`, pass it to the runner.

Expand the `PhaseRunner` interface to include the title:

```go
RunExistingPhase(ctx context.Context, phaseID, beadID, phaseTitle, phaseDescription string, exec ResolvedExecution) (*PhaseRunnerResult, error)
```

Then in the adapters, set `l.CommitSummary = phaseTitle` before running.

**In `executePhase`** (`worker_exec.go`), change the call to pass `phase.Title`:

```go
phaseResult, err := wg.Runner.RunExistingPhase(ctx, phaseID, ps.BeadID, phase.Title, prompt, exec)
```

### 5. Standalone `quasar run` — no changes needed

For standalone runs, `CommitSummary` defaults to `""` and the summary is derived from `state.TaskTitle` (the user's task input). The first line is typically short and descriptive enough.

### 6. Update tests

Update any tests that call `CommitCycle` with the old signature. Update the mock implementations of `CycleCommitter` in tests.

## Files

- `internal/loop/git.go` — update `CycleCommitter` interface and `gitCycleCommitter.CommitCycle`
- `internal/loop/loop.go` — add `CommitSummary` field, pass summary in `runCoderPhase`
- `internal/loop/helpers.go` or inline — add `firstLine` helper
- `internal/nebula/worker.go` — update `PhaseRunner` interface to pass title
- `internal/nebula/worker_exec.go` — pass `phase.Title` to `RunExistingPhase`
- `cmd/nebula_adapters.go` — use `phaseTitle` to set `Loop.CommitSummary`
- `internal/loop/*_test.go` — update mock `CommitCycle` calls

## Acceptance Criteria

- [ ] Cycle commits use format `<beadID>/cycle-<N>: <summary>`
- [ ] In nebula context, summary is the phase title (e.g., "Fix status bar wrapping")
- [ ] In standalone `quasar run`, summary is derived from the task description
- [ ] Summary is truncated to ~72 chars to keep commit messages clean
- [ ] `PhaseRunner` interface passes phase title to the adapters
- [ ] All existing tests pass with updated signatures
- [ ] `go build -o quasar .` succeeds
- [ ] `go vet ./...` passes