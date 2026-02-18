+++
id = "lock-manager"
title = "Implement file-level lock manager for concurrent agents"
type = "feature"
priority = 2
depends_on = ["prior-art-survey"]
+++

## Problem

When multiple quasar workers execute phases in parallel, they may attempt to modify the same files. We need a lock manager that prevents conflicts while allowing maximum parallelism.

## Solution

Design a `LockManager` interface and local implementation:

```go
// LockManager coordinates file access between concurrent agents.
type LockManager interface {
    // Acquire attempts to lock the given paths for the specified owner.
    // Returns an error if any path is already locked by a different owner.
    Acquire(ctx context.Context, owner string, paths []string) error

    // Release releases all locks held by the specified owner.
    Release(ctx context.Context, owner string) error

    // Locked returns the set of currently locked paths and their owners.
    Locked(ctx context.Context) (map[string]string, error)

    // TryAcquire is non-blocking: returns immediately with success or conflict.
    TryAcquire(ctx context.Context, owner string, paths []string) (bool, error)
}
```

### Local Implementation

1. **File-based locks** using a `.quasar/locks/` directory:
   - Each lock is a file containing the owner ID and timestamp
   - Stale lock detection: locks older than a configurable timeout are auto-released
   - Uses OS-level file locking (flock) for atomic lock creation

2. **Scope integration**: When a phase declares a `scope`, the worker acquires locks for all matching files before starting. Scopes are expanded to concrete file lists at lock time.

3. **Deadlock prevention**: Acquire locks in sorted path order to prevent circular waits.

## Files

- `internal/nebula/lock.go` — `LockManager` interface + local file-based impl
- `internal/nebula/lock_test.go` — concurrency tests with goroutines

## Acceptance Criteria

- [ ] `LockManager` interface defined with Acquire, Release, Locked, TryAcquire
- [ ] Local file-based implementation works for single-machine use
- [ ] Stale lock detection and cleanup
- [ ] Deadlock-free acquisition (sorted path order)
- [ ] Concurrent test with multiple goroutines acquiring overlapping paths
- [ ] `go test -race ./internal/nebula/...` passes
