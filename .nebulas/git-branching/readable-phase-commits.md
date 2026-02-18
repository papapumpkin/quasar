+++
id = "readable-phase-commits"
title = "Human-readable phase commit messages with title"
type = "feature"
priority = 3
depends_on = ["readable-cycle-commits"]
+++

## Problem

Phase commit messages are currently `nebula(<name>): <phaseID>`, e.g.:

```
nebula(statusbar-regression): diff-jk-scroll
```

This is functional but the phase ID alone doesn't tell you *what* the phase accomplished. Since cycle commits now have summaries, phase boundary commits should match for consistency.

## Solution

### 1. Update `GitCommitter.CommitPhase` signature

Add `phaseTitle` to the interface:

```go
CommitPhase(ctx context.Context, nebulaName, phaseID, phaseTitle string) error
```

### 2. Update commit message format

In `gitCommitter.CommitPhase`, change from:

```go
msg := fmt.Sprintf("nebula(%s): %s", nebulaName, phaseID)
```

To:

```go
msg := fmt.Sprintf("%s/%s: %s", nebulaName, phaseID, phaseTitle)
```

This produces: `statusbar-regression/diff-jk-scroll: j/k should scroll diff content`

The phase title is truncated to keep the total message under ~80 chars.

### 3. Thread phase title through the caller

In `worker_exec.go`, `executePhase` already has access to `phase.Title`. Update the `CommitPhase` call:

```go
if commitErr := wg.Committer.CommitPhase(ctx, wg.Nebula.Manifest.Nebula.Name, phaseID, phase.Title); commitErr != nil {
```

## Files

- `internal/nebula/git.go` — update `GitCommitter` interface and `gitCommitter.CommitPhase`
- `internal/nebula/worker_exec.go` — pass `phase.Title` to `CommitPhase`

## Acceptance Criteria

- [ ] Phase commits use format `<nebulaName>/<phaseID>: <phaseTitle>`
- [ ] Phase title is truncated to keep commit message reasonable length
- [ ] `go build -o quasar .` succeeds
- [ ] `go vet ./...` passes