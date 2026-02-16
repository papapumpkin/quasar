+++
id = "dynamic-dag-insertion"
title = "Hot-add new phases into the live dependency DAG"
type = "feature"
priority = 1
depends_on = ["phase-change-pipeline"]
+++

## Problem

When a user drops a new `.md` phase file into the nebula directory during execution, it should be picked up, validated, and inserted into the running dependency graph. The new phase might depend on already-completed phases (immediately runnable), in-progress phases (queued), or introduce entirely new dependencies. The DAG resolution logic needs to handle this dynamically without restarting.

## Current State

**DAG resolution** (`internal/nebula/`):
- `Nebula.Phases` is a slice of `PhaseSpec` populated at parse time
- `WorkerGroup.Run()` resolves the execution order at startup based on `depends_on`
- Phases are dispatched in waves — all phases with satisfied dependencies run in parallel
- Once `Run()` starts, no new phases can enter the execution pipeline

**Watcher**:
- `ChangeAdded` is already defined but `handlePhaseAdded` is unimplemented
- `emitChange()` parses the new file and extracts `PhaseID`

**State file** (`nebula.state.toml`):
- Tracks per-phase status (pending/in_progress/done/failed)
- Updated as phases complete

## Solution

### 1. Dynamic Phase Registry

Add a thread-safe phase registry to `WorkerGroup` that can accept new phases at runtime:

```go
type WorkerGroup struct {
    // ... existing fields ...
    phaseMu     sync.Mutex
    livePhases  map[string]*livePhase // all phases (original + hot-added)
    pendingWork chan string            // phaseIDs ready to execute
}

type livePhase struct {
    Spec      PhaseSpec
    Status    PhaseStatus // waiting, running, done, failed
    DependsOn []string
}
```

### 2. Handle ChangeAdded

When `handlePhaseAdded(change)` fires:
1. Parse the new `.md` file into a `PhaseSpec`
2. Validate it (ID uniqueness, frontmatter, etc.)
3. Resolve its `depends_on` against the live phase registry:
   - **All deps done** → immediately queue for execution
   - **Some deps in-progress** → add to registry, will be queued when deps complete
   - **Unknown deps** → log warning, add as blocked (user can fix the file)
4. Update the nebula state file with the new phase (status: pending)
5. Send `MsgPhaseHotAdded{PhaseID, Title, DependsOn}` to the TUI

### 3. DAG Re-evaluation on Phase Completion

When any phase completes, check if newly-added phases are now unblocked:

```go
func (wg *WorkerGroup) onPhaseComplete(phaseID string) {
    wg.phaseMu.Lock()
    defer wg.phaseMu.Unlock()

    for id, p := range wg.livePhases {
        if p.Status != phaseStatusWaiting {
            continue
        }
        if wg.allDepsSatisfied(id) {
            wg.pendingWork <- id
        }
    }
}
```

### 4. Reverse Dependency Insertion

A new phase can also declare that it should run **before** an existing waiting phase by using a `blocks` frontmatter field (optional, in addition to `depends_on`):

```toml
+++
id = "new-middleware"
depends_on = ["setup-models"]
blocks = ["integration-tests"]  # integration-tests now also depends on this
+++
```

When a new phase has `blocks`, inject it as a dependency of the blocked phase — but only if the blocked phase hasn't started yet. If it's already running, log a warning and skip the block.

### 5. TUI Integration

New message types:
```go
type MsgPhaseHotAdded struct {
    PhaseID   string
    Title     string
    DependsOn []string
}
```

The TUI handles this by:
- Adding a new `PhaseEntry` to `NebulaView.Phases`
- Updating the status bar's total count
- Showing a brief notification ("+ new-middleware added to nebula")

### 6. Validation Guardrails

Before inserting a new phase:
- **Cycle detection**: Ensure the new phase doesn't create a dependency cycle
- **ID uniqueness**: Reject if a phase with the same ID already exists
- **Running phase protection**: Never modify dependencies of a phase that's already executing
- **State file sync**: Write updated state immediately so crashes don't lose the addition

## Files to Modify

- `internal/nebula/worker.go` — Add `livePhases` registry, `handlePhaseAdded()`, `onPhaseComplete()` re-evaluation, `pendingWork` channel
- `internal/nebula/types.go` — Add optional `Blocks []string` to `PhaseSpec` frontmatter
- `internal/nebula/parse.go` — Parse `blocks` field from frontmatter
- `internal/nebula/validate.go` — Validate cycle detection with dynamic insertions
- `internal/nebula/state.go` — Write new phase entries to state file
- `internal/tui/msg.go` — Add `MsgPhaseHotAdded`
- `internal/tui/model.go` — Handle `MsgPhaseHotAdded`: append to NebulaView, update status bar
- `internal/tui/nebulaview.go` — Support appending phases after initial render

## Acceptance Criteria

- [ ] New `.md` file dropped into nebula dir is detected and parsed
- [ ] New phase appears in the TUI phase table with correct status
- [ ] Dependencies are resolved: phase runs when all deps are satisfied
- [ ] `blocks` field correctly injects reverse dependencies on waiting phases
- [ ] Cycle detection prevents invalid dependency additions
- [ ] Already-running phases are never modified
- [ ] Status bar total count updates
- [ ] State file is updated immediately
- [ ] `go build` and `go test ./...` pass
