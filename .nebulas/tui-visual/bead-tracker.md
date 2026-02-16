+++
id = "bead-tracker"
title = "Live bead tracker panel showing issue hierarchy per cycle"
type = "feature"
priority = 2
depends_on = ["phase-tree-polish"]
max_review_cycles = 5
max_budget_usd = 30.0
+++

## Problem

Quasar creates and manages beads (epics, tasks, subtasks) as it works — the coder creates task beads, the reviewer creates child issue beads, cycles close and reopen them. But the TUI has no visibility into this. Users have to run `bd list` or `bd show` in a separate terminal to understand the bead state. Since Quasar is becoming an IDE for managing nebulae, the bead lifecycle should be front and center.

## Current State

**Bead operations in the loop** (`internal/loop/loop.go`):
- `l.Beads.Create()` creates the task bead at loop start
- `l.Beads.AddComment()` posts coder/reviewer output as comments
- `l.Beads.Update()` changes assignee between coder/reviewer
- `l.Beads.Close()` closes the bead on approval
- `l.Beads.CreateChild()` creates child beads for reviewer issues (ISSUE: blocks)
- Each child bead has severity, description, and links to the parent task

**Bead client interface** (`internal/beads/beads.go`):
- `Client` interface with `Create`, `Close`, `Update`, `AddComment`, `CreateChild`, `List`, `Show`
- `List` can filter by status, labels, etc.
- `Show` returns full bead detail including children

**What the TUI knows**:
- `MsgTaskStarted{BeadID, Title}` — the task bead ID
- `MsgIssuesFound{Count}` / `MsgPhaseIssuesFound{PhaseID, Count}` — just a count
- Agent output contains the raw ISSUE: blocks but they're not parsed for the TUI
- No message types carry bead hierarchy information

## Solution

### 1. Bead State Messages

Add new message types that carry bead hierarchy snapshots:

```go
// BeadInfo represents a bead's display state.
type BeadInfo struct {
    ID       string
    Title    string
    Status   string // "open", "in_progress", "closed"
    Type     string // "epic", "task", "bug", "feature"
    Priority int
    Children []BeadInfo // nested child issues
}

// MsgBeadUpdate carries the current bead hierarchy for a task.
type MsgBeadUpdate struct {
    TaskBeadID string
    Root       BeadInfo // the task bead with its children
}

// MsgPhaseBeadUpdate carries bead state for a specific phase.
type MsgPhaseBeadUpdate struct {
    PhaseID    string
    TaskBeadID string
    Root       BeadInfo
}
```

### 2. Emit Bead Updates from Bridge

Extend `UIBridge` and `PhaseUIBridge` with a new method that queries bead state and sends updates. Call it after key lifecycle events:
- After task bead creation
- After reviewer creates child issues
- After bead close (approval)
- After each cycle completes

The bridge can call `beads.Show(beadID)` to get the full hierarchy, or we add a new `UI` interface method:

```go
// In ui.UI interface:
BeadUpdate(taskBeadID string, root BeadInfo)
```

The loop calls `l.UI.BeadUpdate()` after creating children, closing beads, etc.

### 3. Bead Tracker View

Create `internal/tui/beadview.go` — a tree view of the bead hierarchy:

```
┌─ Beads: auth-middleware ────────────────────┐
│                                              │
│  ● quasar-a1b  Add JWT auth          open   │
│  ├── ✓ quasar-a1b.1  SQL injection   closed │
│  ├── ◎ quasar-a1b.2  Missing tests   open   │
│  └── ✗ quasar-a1b.3  Error handling  open   │
│                                              │
│  Cycle 1: 3 issues found                    │
│  Cycle 2: 1 issue remaining                 │
└──────────────────────────────────────────────┘
```

Visual treatment:
- Tree connectors (├──, └──) in muted color
- Status icons: `✓` green (closed), `◎` blue (in_progress), `●` white (open), `✗` red (failed)
- Severity indicated by color intensity or a tag (critical=red, major=orange, minor=gray)
- Per-cycle summary line showing how many issues were found/resolved

### 4. Integration into Detail Panel

The bead tracker is a new detail panel mode, toggled with `b` key:
- At `DepthPhases`: shows bead hierarchy for the selected phase's task
- At `DepthPhaseLoop`: shows bead hierarchy for the focused phase, with cycle-by-cycle breakdown
- In loop mode: shows the single task's bead hierarchy

Footer shows `b:beads` toggle hint.

### 5. Live Updates

Bead state updates arrive via `MsgBeadUpdate` / `MsgPhaseBeadUpdate`. The model stores the latest `BeadInfo` per phase:

```go
// In AppModel:
PhaseBeads map[string]BeadInfo // phaseID → latest bead hierarchy
```

When the bead tracker panel is visible and a `MsgPhaseBeadUpdate` arrives for the focused phase, the view refreshes automatically — giving live visibility into issues being created and resolved across cycles.

### 6. Cycle-by-Cycle Breakdown

Track which children were created in which cycle by adding a `Cycle int` field to `BeadInfo` children. Display grouped by cycle:

```
  Cycle 1 — 3 issues
    ✓ quasar-a1b.1  SQL injection        closed
    ◎ quasar-a1b.2  Missing tests        open
    ✗ quasar-a1b.3  Error handling       open
  Cycle 2 — 1 issue
    ● quasar-a1b.4  Incorrect return     open
```

## Files to Create

- `internal/tui/beadview.go` — `BeadView` struct with tree rendering
- `internal/tui/beadview_test.go` — Tests for bead tree rendering and state updates

## Files to Modify

- `internal/tui/msg.go` — Add `BeadInfo`, `MsgBeadUpdate`, `MsgPhaseBeadUpdate`
- `internal/tui/bridge.go` — Add `BeadUpdate` method to `UIBridge` and `PhaseUIBridge`
- `internal/tui/model.go` — Add `PhaseBeads` map; handle bead messages; add `b` key toggle; integrate bead view into detail panel
- `internal/tui/keys.go` — Add `Beads` key binding (`b`)
- `internal/tui/footer.go` — Show `b:beads` hint
- `internal/ui/ui.go` — Add `BeadUpdate` to `UI` interface; no-op in `Printer`
- `internal/loop/loop.go` — Call `l.UI.BeadUpdate()` after bead lifecycle events (create child, close, cycle end)
- `internal/loop/loop_test.go` — Add `BeadUpdate` to mock

## Acceptance Criteria

- [ ] Pressing `b` toggles bead tracker in the detail panel
- [ ] Bead hierarchy shown as a tree with parent task and child issues
- [ ] Status icons: closed (green ✓), open (white ●), in_progress (blue ◎)
- [ ] Child issues grouped by cycle when available
- [ ] Live updates — new issues appear as the reviewer creates them
- [ ] Per-cycle summary (N issues found, M resolved)
- [ ] Works in both loop mode (single task) and nebula mode (per-phase)
- [ ] Footer shows `b:beads` toggle hint
- [ ] `go build` and `go test ./...` pass
