+++
id = "coordinator-interface"
title = "Define the Coordinator interface and ChangeEvent type"
type = "feature"
priority = 2
depends_on = []
scope = ["internal/nebula/coordinator.go"]
+++

## Problem

The current WorkerGroup dispatches phases directly via goroutines with no abstraction for work distribution, change broadcasting, or file locking. To support runtime conflict detection — and later swap in Temporal/etcd — we need a transport-agnostic coordination interface.

## Solution

Create `internal/nebula/coordinator.go` with the Coordinator interface and supporting types.

```go
// ChangeEvent is broadcast by a worker after completing a phase.
type ChangeEvent struct {
    PhaseID      string
    FilesEdited  []string // Actual files modified (from git diff)
    Summary      string   // Human-readable summary of changes
}

// Coordinator manages work distribution and conflict detection between workers.
type Coordinator interface {
    // Enqueue adds a phase to the ready queue.
    Enqueue(ctx context.Context, phaseID string) error
    // Dequeue blocks until a phase is available for work.
    Dequeue(ctx context.Context) (string, error)
    // Broadcast announces completed work to all workers.
    Broadcast(ctx context.Context, event ChangeEvent) error
    // Subscribe returns a channel of change events from other workers.
    Subscribe(ctx context.Context) (<-chan ChangeEvent, error)
    // Lock claims exclusive access to a set of file paths.
    // Returns an unlock function. Blocks until lock is acquired.
    Lock(ctx context.Context, paths []string) (unlock func(), err error)
    // Close shuts down the coordinator and releases resources.
    Close() error
}
```

This interface is designed to be implementable by:
- Go channels (in-process, this nebula)
- etcd (watches + leases for distributed locking)
- Temporal (signals + activities for durable orchestration)

## Files to Modify

- `internal/nebula/coordinator.go` — New file: Coordinator interface + ChangeEvent type

## Acceptance Criteria

- [ ] Interface compiles and is well-documented with GoDoc
- [ ] ChangeEvent has PhaseID, FilesEdited, Summary fields
- [ ] No external dependencies
- [ ] `go vet ./...` passes
