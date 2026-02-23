+++
id = "cockpit-wiring"
title = "Wire fabric into cockpit TUI"
type = "task"
priority = 2
depends_on = ["fabric-cli", "discovery-cli", "tycho-scheduler", "telemetry"]
scope = ["internal/tui/bridge.go", "internal/tui/msg.go", "cmd/nebula_apply.go", "cmd/nebula_adapters.go"]
+++

## Problem

The fabric, discoveries, and telemetry systems exist but have no connection to the cockpit TUI. Hails don't surface as visual interrupts, entanglements aren't displayed in the viewer, and the event stream doesn't drive TUI updates. The cockpit nebula (which builds the visual components) needs a data bridge from fabric to TUI.

## Solution

This phase wires the fabric data into the TUI message bus so the cockpit nebula's views can consume it. It does NOT build the visual components (that's the cockpit nebula's job) — it builds the plumbing.

### New TUI message types

Add to `internal/tui/msg.go`:

```go
// MsgEntanglementUpdate carries entanglement data for the cockpit viewer.
type MsgEntanglementUpdate struct {
    Entanglements []fabric.Entanglement
}

// MsgDiscoveryPosted surfaces a new discovery in the cockpit.
type MsgDiscoveryPosted struct {
    Discovery fabric.Discovery
}

// MsgHail surfaces a human-attention-required interrupt.
type MsgHail struct {
    PhaseID   string
    Discovery fabric.Discovery
}

// MsgScratchpadEntry adds a timestamped note to the scratchpad.
type MsgScratchpadEntry struct {
    Timestamp time.Time
    PhaseID   string
    Text      string
}

// MsgStaleWarning alerts the operator to stale state.
type MsgStaleWarning struct {
    Items []tycho.StaleItem
}
```

### PhaseUIBridge extensions

Extend `PhaseUIBridge` with methods that emit the new message types:

```go
func (b *PhaseUIBridge) EntanglementPublished(entanglements []fabric.Entanglement)
func (b *PhaseUIBridge) DiscoveryPosted(d fabric.Discovery)
func (b *PhaseUIBridge) Hail(phaseID string, d fabric.Discovery)
func (b *PhaseUIBridge) ScratchpadNote(phaseID, text string)
```

Each method calls `b.program.Send(MsgXxx{...})`.

### WorkerGroup / Tycho integration

In `cmd/nebula_adapters.go`, when constructing the `tuiLoopAdapter`:
- Set `Tycho.OnHail` to emit `MsgHail` via the TUI program
- After entanglement publishing, emit `MsgEntanglementUpdate` with the full entanglement list
- After discovery posting, emit `MsgDiscoveryPosted`
- Periodically run `Tycho.StaleCheck` and emit `MsgStaleWarning` if items found

### Telemetry → TUI feed

The telemetry emitter is already writing events. Add a `TelemetryBridge` that reads the JSONL file and converts certain event kinds into scratchpad entries:
- `discovery_posted` → scratchpad entry
- `entanglement_posted` → scratchpad entry
- `pulse_emitted` → scratchpad entry (e.g., "q-1 decision: switched to cursor-based pagination")
- `task_state` transitions → scratchpad entry (e.g., "phase-x: running → review")

This is a lightweight goroutine that tails the telemetry file and sends `MsgScratchpadEntry` to the TUI program.

### AppModel routing

In `AppModel.Update()`, add handlers for the new message types:
- `MsgEntanglementUpdate` → store for contract/entanglement viewer tab
- `MsgDiscoveryPosted` → store for display, show toast notification
- `MsgHail` → trigger decision overlay or gate prompt
- `MsgScratchpadEntry` → append to scratchpad view
- `MsgStaleWarning` → show toast with warning

These handlers store the data. The visual rendering is handled by the cockpit nebula's components (board view, entanglement viewer, scratchpad view, decision overlay).

## Files

- `internal/tui/msg.go` — New message types
- `internal/tui/bridge.go` — PhaseUIBridge extensions
- `internal/tui/telemetry_bridge.go` — Telemetry file tailer → TUI messages
- `cmd/nebula_adapters.go` — Wire Tycho callbacks to TUI message emission
- `cmd/nebula_apply.go` — Pass telemetry emitter to TUI setup
- `internal/tui/model.go` — Message handlers for new types (storage only, no rendering)

## Acceptance Criteria

- [ ] `MsgEntanglementUpdate`, `MsgDiscoveryPosted`, `MsgHail`, `MsgScratchpadEntry`, `MsgStaleWarning` types defined
- [ ] `PhaseUIBridge` emits new message types when fabric events occur
- [ ] `Tycho.OnHail` wired to emit `MsgHail` via the TUI program
- [ ] Telemetry bridge tails JSONL and emits scratchpad entries
- [ ] `AppModel.Update()` handles all new message types (stores data for later rendering)
- [ ] No visual rendering changes — this phase is plumbing only
- [ ] All existing TUI tests continue to pass
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` clean
