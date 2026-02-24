+++
id = "cockpit-graph"
title = "Add Graph tab to cockpit nebula view"
type = "feature"
priority = 2
depends_on = ["dag-renderer"]
scope = ["internal/tui/graphview.go", "internal/tui/tabs.go"]
+++

## Problem

The cockpit has three tabs: Board (kanban columns), Entanglements (fabric interface agreements), and Scratchpad (telemetry notes). The Board view shows workflow state (queued/scanning/running/blocked/done/failed columns) but not the structural dependency relationships between phases.

Users have repeatedly asked "why is this phase waiting?" or "what's the critical path?" and the answer requires mental reconstruction from phase metadata. A visual graph view would make the execution structure immediately comprehensible.

## Solution

### 1. New tab: TabGraph

Add a fourth tab to the cockpit's tab bar. Update `internal/tui/tabs.go`:

```go
const (
    TabBoard         = 0
    TabEntanglements = 1
    TabGraph         = 2  // NEW
    TabScratchpad    = 3  // shifted
)
```

Update the tab labels and key bindings (currently 1/2/3, extend to 1/2/3/4).

### 2. New view: `internal/tui/graphview.go`

```go
// GraphView renders the DAG in the cockpit using the DAGRenderer.
// It re-renders on phase status changes and supports vertical scrolling
// for large graphs.
type GraphView struct {
    renderer  *ui.DAGRenderer
    waves     []dag.Wave
    deps      map[string][]string
    titles    map[string]string
    statuses  map[string]ui.NodeStatus
    viewport  viewport.Model   // lipgloss viewport for scrolling
    width     int
    height    int
}
```

### 3. Live status updates

The GraphView subscribes to the same phase status messages that the NebulaView and BoardView already receive:

- `MsgPhaseTaskStarted` -> status = running
- `MsgPhaseTaskComplete` -> status = done
- `MsgPhaseError` -> status = failed

On each status change, re-render the DAG through the `DAGRenderer` and update the viewport content. Since the DAG structure is static (only statuses change), the layout computation can be cached and only the node styling needs updating.

### 4. Interaction

- **Vertical scroll**: j/k or arrow keys when the graph exceeds viewport height
- **Phase selection**: cursor highlight on nodes; pressing Enter drills down to that phase's LoopView (same as clicking a phase in the Board view)
- **Track toggle**: 't' key toggles track highlighting on/off
- **Critical path toggle**: 'c' key highlights the critical path

### 5. Entanglement annotations (optional enhancement)

When fabric is active, overlay entanglement arrows on the graph edges. An edge from A to B that carries entanglements shows a label:

```
[spacetime-model] ──types: 3, funcs: 2──> [nebula-scanner]
```

This bridges the Graph and Entanglements tabs, showing contracts in context.

### 6. Initialization

In `cmd/tui.go` and `cmd/nebula_apply.go`, when building the TUI program, pass the wave/dependency data to the GraphView during `MsgNebulaInit`:

```go
// In model.go Update() for MsgNebulaInit:
m.GraphView = NewGraphView(waves, deps, titles, width, height)
```

## Files

- `internal/tui/graphview.go` — `GraphView` struct, rendering, interaction handlers
- `internal/tui/tabs.go` — Add `TabGraph` constant, update tab bar rendering and key bindings
- `internal/tui/model.go` — Add `GraphView` field to `AppModel`, wire into `Update()` and `View()`
- `internal/tui/msg.go` — No new messages needed; reuse existing phase status messages

## Acceptance Criteria

- [ ] A "Graph" tab appears in the cockpit tab bar (key: 3 or Tab navigation)
- [ ] The graph renders the DAG with box-drawing characters and status colors
- [ ] Phase status changes are reflected live (running=yellow, done=green, failed=red)
- [ ] Vertical scrolling works for graphs taller than the viewport
- [ ] Pressing Enter on a highlighted node drills down to that phase's LoopView
- [ ] Track highlighting toggles with 't'
- [ ] Critical path highlighting toggles with 'c'
- [ ] Tab ordering is Board(1), Entanglements(2), Graph(3), Scratchpad(4)
- [ ] `go build` and `go vet ./...` pass
