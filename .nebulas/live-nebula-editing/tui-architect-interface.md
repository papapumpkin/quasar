+++
id = "tui-architect-interface"
title = "TUI interface for invoking the nebula architect agent"
type = "feature"
priority = 2
depends_on = ["nebula-architect-agent"]
max_review_cycles = 5
max_budget_usd = 30.0
+++

## Problem

The nebula architect agent can generate and refactor phases, but users need a way to invoke it from the TUI — describe what they want, select dependencies from the live DAG, preview the generated phase, and confirm before it's written to disk.

## Current State

**TUI interaction model**:
- Keyboard-driven: navigation (↑↓), drill-down (enter/esc), toggles (d/i/b)
- Gate prompt overlay for accept/reject/retry/skip decisions
- No text input capability — all interaction is selection-based
- Footer shows context-sensitive keybindings

**Architect agent** (from `nebula-architect-agent` phase):
- `RunArchitect(ctx, invoker, req)` generates/refactors phases
- Returns `ArchitectResult` with parsed phase spec and body
- Needs: user prompt, mode (create/refactor), nebula context

## Solution

### 1. Architect Trigger Keys

Add keybindings at the phase list level:
- `n` — **new phase**: open the architect in create mode
- `e` — **edit/refactor phase**: open the architect in refactor mode for the selected phase (only enabled on in-progress or waiting phases)

### 2. Text Input Overlay

Create a text input overlay for the user to describe what they want:

```
┌─────────────────────────────────────────────┐
│  New Phase                                   │
│                                              │
│  Describe what this phase should do:         │
│  ┌─────────────────────────────────────────┐ │
│  │ Add rate limiting to the API endpoints  │ │
│  │ using a token bucket algorithm. Should  │ │
│  │ integrate with the auth middleware.     │ │
│  └─────────────────────────────────────────┘ │
│                                              │
│  enter:generate  esc:cancel                  │
└─────────────────────────────────────────────┘
```

Use `bubbles/textarea` for multi-line input. For refactor mode, pre-populate with a hint like "What should change about this phase?"

### 3. Architect Working State

While the architect agent runs:
- Show a spinner overlay: "Generating phase... ⠋"
- The agent runs in a goroutine, sends `MsgArchitectResult` when done
- User can press `esc` to cancel (context cancellation)

### 4. Preview & Dependency Picker

After the architect returns, show a preview overlay:

```
┌─────────────────────────────────────────────┐
│  Preview: rate-limiting                      │
│                                              │
│  Title: Add rate limiting to API endpoints   │
│  Type: feature  Priority: 2                  │
│                                              │
│  Dependencies (↑↓ to toggle):                │
│  [✓] setup-models         (done)             │
│  [✓] auth-middleware       (working)          │
│  [ ] integration-tests     (waiting)          │
│  [ ] cleanup-legacy        (waiting)          │
│                                              │
│  ─── Description ──────────────────────────  │
│  Implement token bucket rate limiting...     │
│                                              │
│  enter:confirm  e:edit deps  esc:discard     │
└─────────────────────────────────────────────┘
```

The dependency picker lets the user:
- See the agent's suggested dependencies (pre-checked)
- Toggle dependencies on/off with space bar
- See each phase's current status for context
- Code validates cycle detection in real-time (shows warning if a toggle would create a cycle)

### 5. Confirm & Write

On confirmation:
1. Write the `.md` file to the nebula directory
2. The watcher detects `ChangeAdded` → `dynamic-dag-insertion` handles the rest
3. Close the overlay, return to the phase list
4. New phase appears in the table with "waiting" or "queued" status

### 6. Refactor Flow

For refactor mode (`e` key):
1. Text input pre-populated with "What should change?"
2. Architect receives: current phase body + user's change request
3. Preview shows a diff-like view: what changed in the description
4. On confirm: write updated `.md` → watcher emits `ChangeModified` → graceful refactor pipeline handles injection

### 7. Message Types

```go
type MsgArchitectStart struct {
    Mode    string // "create" or "refactor"
    PhaseID string // for refactor
    Prompt  string // user's description
}
type MsgArchitectResult struct {
    Result *nebula.ArchitectResult
    Err    error
}
type MsgArchitectConfirm struct {
    Result    *nebula.ArchitectResult
    DependsOn []string // user-modified dependencies
}
```

## Files to Create

- `internal/tui/architect_overlay.go` — Text input, working spinner, preview, dependency picker
- `internal/tui/architect_overlay_test.go` — Tests for overlay state machine

## Files to Modify

- `internal/tui/model.go` — Handle architect keys (n/e), manage overlay state, run architect in goroutine
- `internal/tui/keys.go` — Add `NewPhase` (n) and `EditPhase` (e) bindings
- `internal/tui/footer.go` — Show n:new and e:edit hints at phase level
- `internal/tui/msg.go` — Add architect message types

## Acceptance Criteria

- [ ] `n` opens text input for new phase description
- [ ] `e` opens text input for refactoring selected phase
- [ ] Architect agent runs with spinner feedback
- [ ] Preview shows generated phase with editable dependency picker
- [ ] Dependency toggles validate cycle detection in real-time
- [ ] Confirm writes `.md` file and phase appears in the table
- [ ] Refactor writes updated file triggering the refactor pipeline
- [ ] `esc` cancels at any point
- [ ] `go build` and `go test ./internal/tui/...` pass
