+++
id = "panic-recovery"
title = "Add panic recovery to architect goroutine"
type = "bug"
priority = 1
+++

## Bug

BubbleTea runs the architect function in a bare goroutine (`func() tea.Msg { ... }`) with no `recover()`. Any panic (from the data race, a nil pointer, or a bug in the architect code) kills the entire process, including terminal restoration — leaving the terminal in a broken state.

## Root Cause

The `MsgArchitectStart` handler in `internal/tui/model.go` (line ~427) creates a closure:
```go
cmds = append(cmds, func() tea.Msg {
    result, err := fn(ctx, startMsg)
    return MsgArchitectResult{Result: result, Err: err}
})
```

BubbleTea runs this in a goroutine. If `fn` panics, the goroutine dies, `os.Exit` is called by the runtime, and BubbleTea never gets a chance to restore the terminal.

## Fix

### 1. Add `safeArchitectCall()` helper to `internal/tui/model.go`

```go
// safeArchitectCall wraps an architect function call with panic recovery.
// Any panic is converted to an error in the returned MsgArchitectResult.
func safeArchitectCall(fn func(context.Context, MsgArchitectStart) (*nebula.ArchitectResult, error), ctx context.Context, msg MsgArchitectStart) (result *nebula.ArchitectResult, err error) {
    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("architect panic: %v", r)
        }
    }()
    return fn(ctx, msg)
}
```

### 2. Use it in the `MsgArchitectStart` handler

Change the closure from:
```go
result, err := fn(ctx, startMsg)
```
to:
```go
result, err := safeArchitectCall(fn, ctx, startMsg)
```

### 3. Add tests to `internal/tui/model_controls_test.go`

- **Test panic recovery**: set `ArchitectFunc` to a function that calls `panic("boom")`, send `MsgArchitectStart`, verify the returned `tea.Cmd` produces a `MsgArchitectResult` with a non-nil `Err` containing "architect panic" (no crash)
- **Test normal operation preserved**: set `ArchitectFunc` to a function returning a valid result, verify it still works correctly through `safeArchitectCall`

## Files

- `internal/tui/model.go` — add `safeArchitectCall()`, use it in `MsgArchitectStart` handler
- `internal/tui/model_controls_test.go` — add panic recovery tests
