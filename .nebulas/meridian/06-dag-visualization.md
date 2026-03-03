+++
id = "dag-visualization"
title = "Server-side SVG DAG visualization of phase dependency graph"
type = "feature"
priority = 3
depends_on = ["dashboard-page"]
scope = ["internal/web/handler_dag.go", "internal/web/dag_render.go", "internal/web/templates/dag.html"]
+++

## Problem

The TUI has a `GraphView` (`internal/tui/graphview.go`) that renders the phase dependency DAG as a text-based graph with wave-based layout, status coloring, and critical path highlighting. The web dashboard should provide a richer visual representation using SVG, where nodes are clickable and color-coded by phase status.

## Solution

Create a `/dag` route that renders an SVG-based dependency graph. The graph is generated server-side using Go templates (no client-side JS graph library). Nodes are positioned using a wave-based layout algorithm (phases at the same dependency depth share a column), matching the existing `GraphView` wave computation.

### Layout algorithm

Reuse the wave assignment already performed by `nebula.Nebula` / the TUI's `GraphView`:

```go
// internal/web/dag_render.go

// DAGLayout computes node positions for SVG rendering.
type DAGLayout struct {
    Nodes []DAGNode
    Edges []DAGEdge
    Width int
    Height int
}

// DAGNode represents a phase in the SVG graph.
type DAGNode struct {
    ID       string
    Title    string
    Status   string
    Wave     int    // column (dependency depth)
    Row      int    // row within the wave
    X        int    // computed SVG x coordinate
    Y        int    // computed SVG y coordinate
    CSSClass string // "node--done", "node--failed", etc.
    Link     string // href to /phase/{id}
}

// DAGEdge represents a dependency arrow.
type DAGEdge struct {
    FromX, FromY int
    ToX, ToY     int
    CSSClass     string // "edge--satisfied" or "edge--pending"
}

// ComputeDAGLayout assigns SVG coordinates to phases based on wave assignment.
func ComputeDAGLayout(n *nebula.Nebula, state *nebula.State) DAGLayout {
    // 1. Assign waves (topological sort by depends_on).
    // 2. Within each wave, assign row positions.
    // 3. Compute pixel coordinates:
    //    - X = wave * columnWidth + padding
    //    - Y = row * rowHeight + padding
    // 4. Generate edges from each phase to its dependencies.
    const (
        nodeWidth  = 180
        nodeHeight = 50
        colGap     = 60
        rowGap     = 30
        padding    = 40
    )
    // ...
}
```

### SVG template

```html
<!-- internal/web/templates/dag.html -->
<div class="dag-container">
    <svg viewBox="0 0 {{.Layout.Width}} {{.Layout.Height}}"
         class="dag-svg" xmlns="http://www.w3.org/2000/svg">
        <defs>
            <marker id="arrowhead" markerWidth="10" markerHeight="7"
                    refX="10" refY="3.5" orient="auto">
                <polygon points="0 0, 10 3.5, 0 7" fill="#8b949e"/>
            </marker>
        </defs>

        <!-- Edges (rendered first so nodes overlap them) -->
        {{range .Layout.Edges}}
        <line x1="{{.FromX}}" y1="{{.FromY}}"
              x2="{{.ToX}}" y2="{{.ToY}}"
              class="dag-edge {{.CSSClass}}"
              marker-end="url(#arrowhead)"/>
        {{end}}

        <!-- Nodes -->
        {{range .Layout.Nodes}}
        <a href="{{.Link}}">
            <rect x="{{.X}}" y="{{.Y}}"
                  width="180" height="50" rx="8"
                  class="dag-node {{.CSSClass}}"/>
            <text x="{{add .X 90}}" y="{{add .Y 20}}"
                  text-anchor="middle" class="dag-node-id">{{.ID}}</text>
            <text x="{{add .X 90}}" y="{{add .Y 38}}"
                  text-anchor="middle" class="dag-node-title">{{truncate .Title 20}}</text>
        </a>
        {{end}}
    </svg>
</div>
```

### Template functions

Register custom template functions for SVG coordinate arithmetic:

```go
funcMap := template.FuncMap{
    "add":      func(a, b int) int { return a + b },
    "truncate": func(s string, max int) string {
        if len(s) <= max {
            return s
        }
        return s[:max-1] + "..."
    },
}
```

### Live updates via SSE

The DAG page subscribes to `phase-update` SSE events. When a phase status changes, the server pushes an updated SVG `<rect>` with the new CSS class:

```html
<div hx-ext="sse" sse-connect="/events" sse-swap="dag-update" hx-swap="none">
    <!-- Updated rect elements with hx-swap-oob replace their counterparts -->
</div>
```

### Status-to-color mapping

| Status | Fill Color | CSS Class |
|--------|-----------|-----------|
| pending | `#30363d` | `node--pending` |
| in_progress | `#1f6feb` | `node--active` |
| done | `#238636` | `node--done` |
| failed | `#da3633` | `node--failed` |
| skipped | `#8b949e` | `node--skipped` |
| decomposed | `#a371f7` | `node--decomposed` |

### Handler

```go
// internal/web/handler_dag.go

func (s *Server) handleDAG(w http.ResponseWriter, r *http.Request) {
    s.mu.RLock()
    n := s.nebula
    st := s.state
    s.mu.RUnlock()

    layout := ComputeDAGLayout(n, st)
    data := DAGPageData{
        NebulaName: n.Manifest.Nebula.Name,
        Layout:     layout,
    }
    if err := s.templates.ExecuteTemplate(w, "dag.html", data); err != nil {
        http.Error(w, "template error", http.StatusInternalServerError)
    }
}
```

### Route registration

```go
s.mux.HandleFunc("GET /dag", s.handleDAG)
```

## Files

- `internal/web/dag_render.go` — `DAGLayout`, `DAGNode`, `DAGEdge`, `ComputeDAGLayout`, wave assignment, coordinate computation
- `internal/web/dag_render_test.go` — test layout with linear chain, diamond dependencies, disconnected phases
- `internal/web/handler_dag.go` — `handleDAG`, `DAGPageData`
- `internal/web/handler_dag_test.go` — test SVG output contains correct number of nodes and edges
- `internal/web/templates/dag.html` — SVG DAG page template
- `internal/web/sse_bridge.go` — add `dag-update` event type for node status changes
- `internal/web/static/style.css` — SVG node/edge styles, status color classes
- `internal/web/server.go` — register `/dag` route, add template function map

## Acceptance Criteria

- [ ] `GET /dag` returns an HTML page with an embedded SVG dependency graph
- [ ] Each phase renders as a rounded rectangle with ID and truncated title
- [ ] Nodes are color-coded by phase status (pending=gray, active=blue, done=green, failed=red)
- [ ] Dependency edges render as lines with arrowhead markers pointing from dependency to dependent
- [ ] Nodes are clickable and link to `/phase/{id}`
- [ ] Wave-based layout places independent phases in the same column
- [ ] SVG viewport auto-scales to fit the graph size
- [ ] Node colors update in real time via SSE `dag-update` events
- [ ] `go test ./internal/web/...` passes — layout tests verify coordinate computation
