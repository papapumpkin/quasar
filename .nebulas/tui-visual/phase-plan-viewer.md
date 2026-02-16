+++
id = "phase-plan-viewer"
title = "Toggleable phase plan viewer in detail panel"
type = "feature"
priority = 2
depends_on = ["status-bar-redesign"]
+++

## Problem

When reviewing a nebula run, users want to see the original plan/description for each phase — the markdown content from the phase `.md` file. Currently, the detail panel only shows agent output when drilled down. There's no way to view what the phase was supposed to do without leaving the TUI and opening the file.

## Current State

**Detail panel** (`internal/tui/detailpanel.go`):
- `SetContent(title, content string)` accepts raw text
- Used for agent output display when drilled into a cycle/agent
- Only visible at `DepthAgentOutput`

**Phase information available**:
- `PhaseEntry` has `ID`, `Title`, `Status`, `Wave`, `CostUSD`, `Cycles`, `BlockedBy`
- The phase `.md` file content (plan body) is NOT currently loaded into the TUI
- In `cmd/nebula.go`, the parsed `nebula.Nebula` has `Phases` with `Body` field containing the markdown

## Solution

### 1. Pass Phase Plans to TUI

Extend `PhaseInfo` (in `msg.go`) to include the plan body:
```go
type PhaseInfo struct {
    ID        string
    Title     string
    DependsOn []string
    PlanBody  string  // NEW: markdown content from the phase file
}
```

In `cmd/nebula.go`, populate `PlanBody` from the parsed nebula phases when building `[]tui.PhaseInfo`.

### 2. Store Plans in PhaseEntry

Add `PlanBody string` to `PhaseEntry` in `nebulaview.go`. Populate it in `InitPhases()`.

### 3. Toggle Key

Add a keybinding `p` (or `i` for "info") that toggles the detail panel to show the phase plan:
- At `DepthPhases`: pressing the toggle key opens the detail panel with the selected phase's plan body
- At `DepthPhaseLoop`: shows the focused phase's plan
- Pressing the key again hides the plan and returns to previous state

### 4. Detail Panel Mode

Add a `DetailMode` type to track what the detail panel is showing:
```go
type DetailMode int
const (
    DetailHidden DetailMode = iota
    DetailAgentOutput
    DetailPhasePlan
)
```

When `DetailPhasePlan` is active, render the plan body in the detail panel with a "Phase Plan: <id>" title. The footer should show "i:plan" toggle hint.

### 5. Visual Treatment

- Plan body rendered as-is (markdown won't be parsed, just displayed as readable text)
- Title styled with `styleDetailTitle`
- A subtle indicator that this is the plan view (e.g., "📋 Plan" prefix in title or different border color)

## Files to Modify

- `internal/tui/msg.go` — Add `PlanBody` to `PhaseInfo`
- `internal/tui/nebulaview.go` — Add `PlanBody` to `PhaseEntry`; populate in `InitPhases()`
- `internal/tui/model.go` — Add `DetailMode`; handle toggle key; render plan in detail panel
- `internal/tui/keys.go` — Add `Info` key binding (`i`)
- `internal/tui/footer.go` — Show "i:plan" hint at phase-level views
- `cmd/nebula.go` — Populate `PlanBody` from parsed nebula phases

## Acceptance Criteria

- [ ] Pressing `i` at phase level opens/closes the plan viewer in the detail panel
- [ ] Plan shows the phase's markdown body text
- [ ] Plan viewer is visible alongside the phase list (no navigation depth change required)
- [ ] Footer shows "i:plan" toggle hint when at phase level
- [ ] Works for both DepthPhases (selected phase) and DepthPhaseLoop (focused phase)
- [ ] Toggling off returns to previous detail panel state
- [ ] `go build` and `go test ./internal/tui/...` pass
