+++
id = "contract-viewer"
title = "Contracts tab view"
type = "feature"
priority = 2
depends_on = ["tab-system"]
scope = ["internal/tui/contractview.go", "internal/tui/contractview_test.go"]
+++

## Problem

When multiple phases work in parallel, they often share interfaces — a function signature, a struct definition, a database schema. The cockpit mockup shows a "Contracts" tab that displays these interface agreements between coupled phases, so the operator can see at a glance what's been agreed upon and whether any contracts are in dispute.

Currently there is no TUI representation of cross-phase interface contracts.

## Solution

Create a `ContractView` component that renders in the `TabContracts` tab. It displays a scrollable list of contract cards, each showing:

1. **Contract ID** (e.g., `contract-01`) as the card title in `colorBlueshift`
2. **Parties**: the two phase IDs that share the contract, connected with `↔` in `colorAccent`
3. **Status**: `fulfilled` (green), `in progress` (yellow), or `disputed` (red)
4. **Interface body**: the shared type/function signatures rendered as syntax-highlighted Go code

Each contract is rendered in a bordered box using `lipgloss.RoundedBorder()` with `colorMuted` borders.

```go
type Contract struct {
    ID       string
    PhaseA   string
    PhaseB   string
    Status   ContractStatus // Fulfilled, InProgress, Disputed
    Body     string         // Go interface/type definitions
}

type ContractStatus int
const (
    ContractInProgress ContractStatus = iota
    ContractFulfilled
    ContractDisputed
)

type ContractView struct {
    contracts []Contract
    cursor    int
    viewport  viewport.Model
    width     int
    height    int
}
```

**Data source**: For this phase, contracts are populated from a new `MsgContractUpdate` message type. The message carries the contract data. The contract-board nebula (if completed) provides the backend; otherwise, the view works with whatever contract data is sent to it. The view is a pure consumer — it never writes contract data.

**Navigation**: Arrow keys scroll through contracts. The viewport supports scrolling for long interface definitions. `Esc` returns to the tab bar.

**Future enhancement** (not in this phase): Inline editing of contracts directly in the TUI, with changes propagated to workers at their next checkpoint. For now, the view is read-only.

## Files

- `internal/tui/contractview.go` — `ContractView` component with contract cards and scrollable viewport
- `internal/tui/contractview_test.go` — Tests for contract rendering, status styling, and cursor navigation

## Acceptance Criteria

- [ ] Contract cards render with ID, parties, status, and interface body
- [ ] Status colors match the galactic theme (green/yellow/red)
- [ ] Interface body renders as monospace code text
- [ ] Cursor navigation scrolls through contracts
- [ ] View integrates with the tab system (appears on `TabContracts`)
- [ ] New `MsgContractUpdate` message type defined in `msg.go`
- [ ] View handles zero contracts gracefully (shows "No contracts" placeholder)
- [ ] `go test ./internal/tui/...` passes
- [ ] `go vet ./...` clean
