+++
id = "dag-renderer"
title = "ASCII DAG visualization component"
type = "feature"
priority = 2
depends_on = []
scope = ["internal/ui/dagrender.go"]
+++

## Problem

The cockpit TUI shows phases in two views: a flat table (NebulaView) and a kanban board (BoardView). Neither shows the actual dependency graph — users can't see which phases block which, where the critical path is, or how tracks relate to each other.

The DAG engine computes waves, tracks, and impact scores, but this rich structural information is never visualized. Users have to mentally reconstruct the graph from `depends_on` arrays.

A visual DAG renderer is needed as a standalone component that can be used in both the TUI (lipgloss-styled) and stderr (plain ANSI) contexts.

## Solution

### 1. New file: `internal/ui/dagrender.go`

Build a box-and-arrow ASCII renderer for DAGs:

```go
// DAGRenderer produces an ASCII visualization of a directed acyclic graph.
// Nodes are drawn as boxes with status-colored borders. Edges are drawn
// with Unicode line-drawing characters.
type DAGRenderer struct {
    Width      int           // available terminal width
    UseColor   bool          // whether to emit ANSI colors
    StatusFunc func(id string) NodeStatus // callback for live status
}

type NodeStatus struct {
    State string // "queued", "running", "done", "failed", "blocked"
    Cost  float64
    Cycles int
}

// Render produces the ASCII DAG as a string.
func (r *DAGRenderer) Render(waves []dag.Wave, deps map[string][]string, titles map[string]string) string
```

### 2. Layout algorithm

Use a wave-based layout since waves are already computed:

```
Wave 1:  [spacetime-model]
              |
         _____|_____
        |           |
Wave 2: [spacetime  [nebula-
         -lock]      scanner]
                       |
Wave 3:          [catalog-
                  reports]
                     |
Wave 4:          [agent-
                  synthesis]
                     |
Wave 5:          [cli-
                  relativity]
```

**Layout steps:**
1. Place nodes in their wave rows
2. Within each wave, center nodes horizontally with spacing
3. Draw vertical connectors between waves for dependencies
4. For multi-parent nodes, draw branching connectors using `├`, `┤`, `┬`, `┴`, `─`, `│`
5. Color node boxes by status: green=done, yellow=running, blue=queued, red=failed, orange=blocked

### 3. Node rendering

Each node is a compact box:

```
┌─────────────────┐
│ spacetime-model  │   <- title, colored by status
│ $2.34  3/5 cyc  │   <- cost + cycles (optional, only in live mode)
└─────────────────┘
```

For compact mode (many phases), use single-line nodes:

```
[spacetime-model] ─> [nebula-scanner] ─> [catalog-reports]
                 \─> [spacetime-lock]
```

### 4. Track highlighting

When rendering tracks, use distinct border styles or subtle background colors to visually group nodes by track:

- Track 0: solid borders `┌─┐`
- Track 1: double borders `╔═╗`
- Or use colored borders per track

### 5. Critical path

Highlight the critical path (longest chain) with bold or bright styling. The critical path determines the minimum execution time regardless of parallelism.

### 6. Dual-context support

The renderer outputs a plain string. For TUI usage, the caller wraps it in a lipgloss-styled viewport. For stderr usage, it's printed directly. The `UseColor` flag controls ANSI escape codes.

## Files

- `internal/ui/dagrender.go` — `DAGRenderer`, layout algorithm, box drawing, connector logic
- `internal/ui/dagrender_test.go` — Tests for layout, rendering, edge cases (single node, diamond, wide fan-out)

## Acceptance Criteria

- [ ] Renderer produces correct ASCII DAG for linear chains, diamonds, and fan-out/fan-in patterns
- [ ] Nodes are colored by status when `UseColor = true`
- [ ] Critical path is visually highlighted
- [ ] Compact mode renders single-line nodes for large DAGs (>10 phases)
- [ ] Works within terminal widths from 80 to 200 columns
- [ ] Output is deterministic (same input always produces same output)
- [ ] No lipgloss dependency — the renderer produces plain strings with optional ANSI
- [ ] `go vet ./...` passes
- [ ] Table-driven tests cover: 1 node, 2-node chain, diamond, 3-way fan-out, 10+ node graph
