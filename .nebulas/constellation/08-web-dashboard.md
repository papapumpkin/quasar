+++
id = "web-dashboard"
title = "Constellation view in the meridian web dashboard"
type = "feature"
priority = 3
depends_on = ["cli-commands"]
scope = ["internal/web/constellation.go", "internal/web/constellation_test.go", "internal/web/templates/constellation.html"]
+++

## Problem

The meridian web dashboard (nebula 2) provides visibility into individual nebula execution. When running a constellation, users need a higher-level view: the DAG of nebulas with status, cost, and progress per nebula, plus drill-down into individual nebula views. Without this, users monitoring a constellation in the browser have no way to see the overall picture.

## Solution

### Routes

```go
// Constellation web routes:
// GET  /constellation                — constellation overview (DAG + status table)
// GET  /constellation/{name}         — detail view for a specific nebula within constellation
// GET  /constellation/dag            — SVG DAG of nebula dependencies (HTMX partial)
// GET  /constellation/oracle         — oracle decision log
```

### Constellation overview page

The overview page has two sections: an SVG DAG visualization (reusing the pattern from meridian phase 6) and a status table listing each nebula with its status, cost, attempts, and progress.

```html
<!-- internal/web/templates/constellation.html -->
<div class="constellation-layout">
    <h1>Constellation: {{.Name}}</h1>

    <div class="constellation-dag"
         hx-get="/constellation/dag"
         hx-trigger="load, sse:constellation-update"
         hx-swap="innerHTML">
        <!-- SVG DAG loaded via HTMX -->
    </div>

    <table class="constellation-table" id="constellation-status">
        <thead>
            <tr>
                <th>Nebula</th>
                <th>Status</th>
                <th>Cost</th>
                <th>Attempts</th>
                <th>Duration</th>
                <th></th>
            </tr>
        </thead>
        <tbody>
            {{range .Nebulas}}
            <tr id="nebula-row-{{.Name}}" class="nebula-row nebula-row--{{.Status}}">
                <td><a href="/constellation/{{.Name}}">{{.Name}}</a></td>
                <td>{{.StatusIcon}} {{.Status}}</td>
                <td>${{printf "%.4f" .CostUSD}}</td>
                <td>{{.Attempts}}</td>
                <td>{{.Duration}}</td>
                <td>
                    {{if eq .Status "running"}}
                    <a href="/dashboard?nebula={{.Name}}">Live View</a>
                    {{end}}
                </td>
            </tr>
            {{end}}
        </tbody>
    </table>
</div>
```

### SVG DAG visualization

Reuse the pattern from meridian phase 6 (phase DAG) but at the nebula level. Each node represents a nebula, colored by status:

```go
func (s *Server) handleConstellationDAG(w http.ResponseWriter, r *http.Request) {
    state := s.constellationState()
    if state == nil {
        http.Error(w, "no constellation running", http.StatusNotFound)
        return
    }

    svg := renderConstellationDAG(s.constellation, state)
    w.Header().Set("Content-Type", "image/svg+xml")
    w.Write([]byte(svg))
}
```

Node colors follow the galactic palette:
- Pending: `colorMuted` (gray)
- Running: `colorAccent` (amber)
- Done: `colorSuccess` (green)
- Failed: `colorDanger` (red)
- Skipped: `colorMuted` with strikethrough

### SSE integration

Constellation-specific SSE events update the dashboard in real time:

```go
// Constellation SSE events:
// "constellation-update"   — nebula status changed (triggers table row + DAG refresh)
// "constellation-oracle"   — oracle made a decision (appends to decision log)
// "constellation-done"     — constellation execution complete
```

The constellation status table rows are updated via `hx-swap-oob`:

```go
func (s *Server) broadcastConstellationUpdate(name string, status NebulaStatus) {
    var buf strings.Builder
    s.templates.ExecuteTemplate(&buf, "constellation-row", nebulaRowData{
        Name: name, Status: status, /* ... */
    })
    s.broadcast(SSEEvent{
        Type: "constellation-update",
        Data: buf.String(),
    })
}
```

### Oracle decision log

```html
<div class="oracle-log" id="oracle-log"
     sse-swap="constellation-oracle" hx-swap="beforeend">
    {{range .OracleDecisions}}
    <div class="oracle-entry oracle-entry--{{.Decision.Strategy}}">
        <span class="oracle-time">{{.Timestamp.Format "15:04"}}</span>
        <span class="oracle-nebula">{{.NebulaName}}</span>
        <span class="oracle-strategy">{{.Decision.Strategy}}</span>
        <span class="oracle-reason">{{.Decision.Reason}}</span>
    </div>
    {{end}}
</div>
```

### Nebula detail drill-down

`GET /constellation/{name}` renders a page that embeds the individual nebula's dashboard (phases, cycles, diffs) within the constellation context, with a breadcrumb navigation back to the constellation overview.

### Handler registration

```go
// In server.go:
s.mux.HandleFunc("GET /constellation", s.handleConstellationOverview)
s.mux.HandleFunc("GET /constellation/dag", s.handleConstellationDAG)
s.mux.HandleFunc("GET /constellation/oracle", s.handleConstellationOracle)
s.mux.HandleFunc("GET /constellation/{name}", s.handleConstellationNebula)
```

## Files

- `internal/web/constellation.go` — `handleConstellationOverview`, `handleConstellationDAG`, `handleConstellationOracle`, `handleConstellationNebula`, `broadcastConstellationUpdate`, `renderConstellationDAG`
- `internal/web/constellation_test.go` — tests for: overview renders nebula table, DAG returns valid SVG, oracle log renders decisions, status colors match nebula state
- `internal/web/templates/constellation.html` — constellation overview page with DAG, status table, oracle log
- `internal/web/templates/partials/constellation_row.html` — per-nebula table row partial for OOB swaps
- `internal/web/server.go` — register constellation routes, add `constellation` field to Server

## Acceptance Criteria

- [ ] `GET /constellation` renders overview with nebula status table and SVG DAG
- [ ] DAG visualization shows nebula nodes colored by execution status
- [ ] DAG edges show dependency relationships between nebulas
- [ ] Status table updates in real time via SSE as nebulas complete/fail
- [ ] Oracle decision log shows timestamped decisions with strategy and reason
- [ ] Oracle decisions appear via SSE without page refresh
- [ ] Clicking a nebula name navigates to its individual dashboard
- [ ] Running nebulas have a "Live View" link to the execution dashboard
- [ ] `constellation-done` SSE event signals completion
- [ ] `go test ./internal/web/...` passes
- [ ] `go vet ./...` passes
