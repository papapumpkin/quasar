+++
id = "architect-wiring"
title = "Wire up ArchitectFunc in NewNebulaProgram"
type = "bug"
priority = 1
+++

## Bug

Pressing `n` (new phase) or `e` (edit phase) in nebula mode shows "architect not available" because `ArchitectFunc` is never set on `AppModel`. `NewNebulaProgram()` doesn't accept or wire an architect function.

## Root Cause

Both `handleNewPhaseKey()` and `handleEditPhaseKey()` set `m.Architect` (creating the overlay), which triggers `MsgArchitectStart`. The handler at line 418-435 checks `m.ArchitectFunc != nil` — since it's never set, it falls through to the error toast.

## Fix

Add an `architectFunc` parameter to `NewNebulaProgram()` and set `model.ArchitectFunc = architectFunc`.

In `cmd/nebula.go`, construct a closure that calls `nebula.RunArchitect()` with the claude invoker and the loaded nebula, then pass it to `NewNebulaProgram()`.

## Files

- `internal/tui/tui.go` — add parameter to `NewNebulaProgram()`
- `cmd/nebula.go` — construct and pass the architect closure
