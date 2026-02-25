+++
id = "ui-hail-interface"
title = "Extend UI interface with hail notification methods"
type = "feature"
priority = 2
depends_on = ["hail-types"]
+++

## Problem

The `ui.UI` interface has no methods for surfacing agent hails to the human operator. Without this, hails are collected but invisible.

## Solution

Add two methods to the `ui.UI` interface:

```go
HailReceived(h Hail)           // A new hail has been posted
HailResolved(id, resolution string)  // A hail was resolved by the human
```

Implement them in both output paths:

### Stderr Printer (`internal/ui/ui.go`)
- `HailReceived`: Print a formatted, attention-grabbing block to stderr:
  ```
  ⚠ AGENT NEEDS INPUT [decision_needed] — cycle 3, coder
  Summary: Unsure whether to use mutex or channel for synchronization
  Detail: (truncated context)
  Options: A) sync.Mutex  B) channel  C) Skip — proceed with best guess
  ```
- `HailResolved`: Print a brief confirmation line.

### TUI Bridge (`internal/tui/bridge.go`)
- Send `MsgHailReceived` and `MsgHailResolved` messages to the BubbleTea program.

## Files

- `internal/ui/ui.go` — Add `HailReceived` and `HailResolved` to UI interface
- `internal/ui/ui.go` — Implement in Printer
- `internal/tui/msg.go` — Add `MsgHailReceived` and `MsgHailResolved` message types
- `internal/tui/bridge.go` — Implement bridge methods for both UIBridge and PhaseUIBridge

## Acceptance Criteria

- [ ] UI interface has HailReceived and HailResolved methods
- [ ] Printer formats hails as attention-grabbing stderr output
- [ ] TUI bridge sends typed messages for both events
- [ ] Existing code compiles (all UI implementations updated)