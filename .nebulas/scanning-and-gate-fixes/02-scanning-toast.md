+++
id = "scanning-toast"
title = "Surface fabric scanning state as a toast notification"
type = "task"
priority = 2
depends_on = ["remove-scanning-column"]
labels = ["quasar", "tui"]
scope = ["internal/tui/msg.go", "internal/tui/bridge.go"]
allow_scope_overlap = true
+++

## Problem

When the fabric layer transitions a phase from blocked → scanning → running, the scanning step is invisible in the TUI. The Tycho scheduler sets `fabric.StateScanning` in `Reevaluate()` when a phase's blockers are resolved, and `fabric.StateRunning` in `flatScan()`/`ScanWaves()` when polling succeeds. But the worker dispatch loop (`worker.go:355-365`) never notifies the TUI between these states — the first TUI update is `MsgPhaseTaskStarted` which maps directly to `PhaseWorking`/`ColRunning`.

This means users never see phases being evaluated for readiness, making the concurrency model feel opaque. A brief toast like `"[phase-id] scanning entanglements..."` would surface this transition without needing a dedicated board column.

## Solution

Add a new `MsgPhaseScanning` message type and wire it from the worker dispatch loop to the TUI as a toast notification.

### Changes

1. **`internal/tui/msg.go`** — Add a new message type:
   ```go
   // MsgPhaseScanning is sent when a phase enters the fabric scanning gate.
   type MsgPhaseScanning struct {
       PhaseID string
   }
   ```

2. **`internal/tui/model.go`** — Handle `MsgPhaseScanning` in `Update()` by creating a non-error toast:
   ```go
   case MsgPhaseScanning:
       toast, cmd := NewToast(fmt.Sprintf("[%s] scanning entanglements", msg.PhaseID), false)
       m.Toasts = append(m.Toasts, toast)
       cmds = append(cmds, cmd)
   ```

3. **`internal/tui/bridge.go`** — Add a `PhaseScanning(phaseID string)` method to `PhaseUIBridge` that sends `MsgPhaseScanning`. Alternatively, add a dedicated method on the `tea.Program` that the worker can call directly.

4. **Worker notification** — In the worker dispatch loop (`internal/nebula/worker.go`), after `Eligible()` returns phases and before `Scan()` filters them, send a scanning notification for each eligible phase. This requires the TUI program reference to be accessible from the worker context. The cleanest approach:
   - Add an `OnScanning func(phaseID string)` callback to `WorkerGroup` (similar to `OnProgress`, `OnRefactor`, `OnHotAdd`)
   - In `cmd/nebula_apply.go`, wire it to `program.Send(tui.MsgPhaseScanning{PhaseID: id})`
   - In the dispatch loop, call `wg.OnScanning(id)` for each eligible phase before `Scan()`

### Toast Behavior

The toast auto-dismisses after the standard timeout (typically 3-5 seconds). Since scanning is sub-second, the toast will likely appear briefly and dismiss before the phase even starts running — which is exactly the right UX. If multiple phases scan simultaneously, each gets its own toast.

## Files

- `internal/tui/msg.go` — Add `MsgPhaseScanning` message type
- `internal/tui/model.go` — Handle `MsgPhaseScanning` as a toast in `Update()`
- `internal/tui/bridge.go` — Optionally add `PhaseScanning` to bridge (or wire directly via callback)
- `internal/nebula/worker.go` — Add `OnScanning` callback, call it before `Scan()` in the dispatch loop
- `cmd/nebula_apply.go` — Wire `OnScanning` to send `MsgPhaseScanning` to the TUI program

## Acceptance Criteria

- [ ] When a phase enters the scanning gate, a toast notification appears: `"[phase-id] scanning entanglements"`
- [ ] The toast auto-dismisses after the standard timeout
- [ ] Multiple concurrent scanning toasts display correctly (stacked)
- [ ] When fabric is not configured (legacy mode), no scanning toasts appear
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] All existing tests pass
