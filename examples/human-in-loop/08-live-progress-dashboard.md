+++
id = "live-progress-dashboard"
title = "Build a live progress dashboard via OnProgress"
type = "feature"
priority = 3
depends_on = ["rename-task-to-phase"]
+++

## Problem

The `OnProgress ProgressFunc` callback exists on `WorkerGroup` but provides minimal information. Humans running a nebula have no real-time visibility into what's happening — which phases are running, completed, blocked, or waiting.

## Solution

Enhance the progress reporting to render a live-updating dashboard to stderr using ANSI escape codes.

### Dashboard Layout

```
Nebula: CI/CD Pipeline          [3/8 done, 1 active]
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  [done] 01 test-script-action     $0.12  2 cycles
  [done] 02 vet-script-action      $0.08  1 cycle
  [>>>>] 03 lint-script-action     $0.04  cycle 1...
  [wait] 04 fmt-script-action
  [wait] 05 security-script-action
  [wait] 06 build-script-action
  [gate] 07 ci-workflow            (blocked: 01-06)
  [gate] 08 release-workflow       (blocked: 07)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Budget: $0.24 / $50.00
```

### Status Icons

| Status | Display | Color |
|--------|---------|-------|
| `done` | `[done]` | Green |
| `in_progress` | `[>>>>]` | Cyan (animated dots) |
| `pending` (unblocked) | `[wait]` | Dim white |
| `pending` (blocked) | `[gate]` | Yellow |
| `failed` | `[FAIL]` | Red |
| `skipped` | `[skip]` | Dim |

### Implementation

Create a `Dashboard` struct in `internal/nebula/dashboard.go`:

```go
type Dashboard struct {
    Writer    io.Writer  // stderr
    Nebula    *Nebula
    State     *State
    Budget    float64
    IsTTY     bool       // Controls whether to use ANSI cursor movement
}
```

- `Render()` — Draw the full dashboard
- `Update(phaseID string, status PhaseStatus)` — Update a single phase's status and re-render
- Use ANSI cursor-up (`\033[<n>A`) to overwrite previous output in TTY mode
- In non-TTY mode, print a simple one-line status update per change (no cursor movement)

### Integration

Wire `Dashboard.Update` as the `OnProgress` callback in `WorkerGroup`. The dashboard owns the rendering; the worker just calls the callback with phase status changes.

### Constraints

- No external TUI frameworks — use raw ANSI codes via `ui.Printer` helpers
- Must not interfere with gate prompts (clear/pause dashboard when prompting)
- Thread-safe (multiple workers may update concurrently)

## Files to Create

- `internal/nebula/dashboard.go` — `Dashboard` struct and rendering
- `internal/nebula/dashboard_test.go` — Test rendering output

## Files to Modify

- `internal/nebula/worker.go` — Wire dashboard as `OnProgress` callback
- `internal/ui/printer.go` — Add ANSI cursor movement helpers if needed

## Acceptance Criteria

- [ ] Dashboard renders all phases with status, cost, and cycle count
- [ ] Live updates overwrite previous output in TTY mode
- [ ] Non-TTY mode falls back to simple line-per-update output
- [ ] Thread-safe for concurrent worker updates
- [ ] Pauses rendering during gate prompts to avoid visual conflicts
- [ ] Budget tracking shown at bottom
- [ ] `go test ./internal/nebula/...` passes