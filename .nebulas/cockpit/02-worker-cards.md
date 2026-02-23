+++
id = "worker-cards"
title = "Live worker detail cards"
type = "feature"
priority = 2
depends_on = ["board-view"]
scope = ["internal/tui/workercard.go", "internal/tui/workercard_test.go"]
+++

## Problem

When phases are running, the current TUI shows only a status icon and spinner in the phase row. There's no at-a-glance view of *what each worker is doing right now* — which cycle it's on, how many tokens it's burned, which files it has locked, or whether the coder or reviewer is active.

The cockpit mockup shows detail cards beneath the board columns — one card per active worker — with a compact summary of live state.

## Solution

Create a `WorkerCard` component that renders a bordered box for each phase in the `Running` state. Cards are displayed below the board columns in a horizontal stack (up to `max_workers` cards side by side).

Each card contains:
- **Phase name** as the card title (styled with `colorAccent`)
- **Worker ID** (e.g., `w-1`, `w-2`) in `colorNebula`
- **Cycle** counter: `2/5` — current cycle / max cycles
- **Tokens** spent so far for this phase
- **Files** — scope glob or list of modified files (truncated)
- **Activity line** — current state: `coding...`, `reviewing...`, `compiling...` in appropriate color

The card uses a `lipgloss.NewStyle().Border(lipgloss.RoundedBorder())` with `colorMuted` border. Width is `terminal_width / max_workers` (clamped to min 30, max 50 chars).

```go
type WorkerCard struct {
    PhaseID    string
    WorkerID   string
    Cycle      int
    MaxCycles  int
    TokensUsed int
    Files      []string
    Activity   string
    AgentRole  string // "coder" or "reviewer"
}
```

Data for worker cards comes from existing `MsgPhaseAgentStart`, `MsgPhaseCycleStart`, and `MsgPhaseAgentDone` messages — no new message types needed. `AppModel` maintains a `map[string]*WorkerCard` keyed by phase ID, populated on message receipt.

Cards appear only when the board view is active. On narrow terminals, cards stack vertically instead of horizontally.

## Files

- `internal/tui/workercard.go` — `WorkerCard` rendering component
- `internal/tui/workercard_test.go` — Tests for card rendering and data population

## Acceptance Criteria

- [ ] One card rendered per active (Running) phase
- [ ] Cards show worker ID, cycle counter, token spend, files, and activity
- [ ] Cards use rounded border with galactic color theme
- [ ] Cards stack horizontally up to terminal width, then wrap vertically
- [ ] Cards appear/disappear as phases transition in/out of Running state
- [ ] No new message types introduced — reuses existing `MsgPhase*` messages
- [ ] `go test ./internal/tui/...` passes
- [ ] `go vet ./...` clean
