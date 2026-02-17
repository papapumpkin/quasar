+++
id = "snapshot-phases"
title = "Add Nebula.Snapshot() and fix architect data race"
type = "bug"
priority = 1
+++

## Bug

`buildArchitectFunc` in `cmd/nebula.go` captures `n *nebula.Nebula` by pointer. The architect goroutine reads `n.Phases` via `buildArchitectPrompt()`. Concurrently, `handlePhaseAdded()` in `worker.go:1013` does `wg.Nebula.Phases = append(...)` under `wg.mu`. The architect goroutine has no lock — this is a data race on the slice header that can panic and crash the TUI.

## Root Cause

The architect closure holds a raw `*Nebula` pointer. When it reads `Nebula.Phases` to build the prompt (iterating the slice, reading `PhaseSpec.DependsOn`, `Labels`, `Scope`, `Blocks` sub-slices), a concurrent `append` in `handlePhaseAdded` can reallocate the slice, causing the architect goroutine to read freed memory.

## Fix

### 1. Add `Nebula.Snapshot()` to `internal/nebula/types.go`

Add a method `func (n *Nebula) Snapshot() *Nebula` that returns a deep copy:
- Copy the struct value
- Create a new `Phases` slice with independent `PhaseSpec` values
- For each `PhaseSpec`, deep-copy the sub-slices: `DependsOn`, `Labels`, `Scope`, `Blocks` (these are `[]string` — use `slices.Clone()` or manual `append([]string{}, orig...)`)
- Scalar fields and the `Manifest` struct can be shallow-copied (they're value types)

### 2. Add `WorkerGroup.SnapshotNebula()` to `internal/nebula/worker.go`

Add a method `func (wg *WorkerGroup) SnapshotNebula() *Nebula` that:
- Locks `wg.mu`
- Calls `wg.Nebula.Snapshot()`
- Unlocks `wg.mu`
- Returns the snapshot

This must be exported so `cmd/nebula.go` can use it.

### 3. Change `buildArchitectFunc` in `cmd/nebula.go`

Change the signature from:
```go
func buildArchitectFunc(invoker agent.Invoker, n *nebula.Nebula) ...
```
to:
```go
func buildArchitectFunc(invoker agent.Invoker, snapshotFn func() *nebula.Nebula) ...
```

The returned closure calls `snapshotFn()` to get a race-free copy of the nebula at call time, then passes it to `nebula.RunArchitect()`.

### 4. Update call sites in `cmd/nebula.go`

Both call sites (line ~259 and ~406) currently pass `n` (or `nextN`). Change them to pass `wg.SnapshotNebula` (the method value, which is already a `func() *nebula.Nebula`).

### 5. Add tests in `internal/nebula/types_test.go`

- **Test snapshot independence**: create a `Nebula`, snapshot it, append to original `Phases` — verify snapshot `Phases` length unchanged
- **Test deep copy of sub-slices**: snapshot, modify original's `Phases[0].DependsOn` — verify snapshot's copy is unaffected
- **Test nil Phases**: snapshot a `Nebula` with nil `Phases` — no panic

## Files

- `internal/nebula/types.go` — add `Snapshot()` method
- `internal/nebula/types_test.go` — new file with snapshot tests
- `internal/nebula/worker.go` — add `SnapshotNebula()` method
- `cmd/nebula.go` — change `buildArchitectFunc` signature and update call sites
