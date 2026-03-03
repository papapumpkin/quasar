+++
id = "phase-detail"
title = "Phase detail page with cycle timeline and agent output"
type = "feature"
priority = 2
depends_on = ["sse-live-updates"]
scope = ["internal/web/handler_phase.go", "internal/web/templates/phase_detail.html"]
+++

## Problem

The dashboard shows a summary table, but operators need to drill into individual phases to see what the coder and reviewer agents produced, how many cycles ran, what issues the reviewer found, and how costs accumulated. The TUI provides this via `LoopView` (cycle-by-cycle timeline with `AgentEntry` and `CycleEntry` in `internal/tui/loopview.go`) and `DetailPanel` (agent output with syntax highlighting in `internal/tui/detailpanel.go`). The web dashboard needs an equivalent per-phase detail page.

## Solution

Create a `/phase/{id}` route that renders a detail page with:

1. **Phase header**: ID, title, status, total cost, total cycles, blocked-by list
2. **Cycle timeline**: Ordered list of cycles, each showing coder and reviewer entries
3. **Agent entries**: For each agent in a cycle — role, cost, duration, issue count, truncated output
4. **Reviewer assessment**: Satisfaction score, risk level, summary (from `MsgPhaseCycleSummary`)

### Data model

The server needs to maintain per-phase cycle history. Add a `PhaseDetail` cache to the `Server` struct that accumulates events as they arrive:

```go
// internal/web/phase_state.go

// PhaseDetail holds accumulated cycle data for a single phase.
type PhaseDetail struct {
    ID         string
    Title      string
    Status     string
    TotalCost  float64
    Cycles     []CycleDetail
    BlockedBy  []string
    StartedAt  time.Time
    CompletedAt time.Time
}

// CycleDetail holds data for one coder-reviewer cycle.
type CycleDetail struct {
    Number     int
    Agents     []AgentDetail
    Summary    *CycleSummary
}

// AgentDetail mirrors tui.AgentEntry for web rendering.
type AgentDetail struct {
    Role       string // "coder" or "reviewer"
    CostUSD    float64
    DurationMs int
    IssueCount int
    Output     string // truncated agent output
    Done       bool
}

// CycleSummary holds reviewer assessment data.
type CycleSummary struct {
    Satisfaction string // "satisfied", "unsatisfied", etc.
    Risk         string // "low", "medium", "high"
    Summary      string
    IssueCount   int
}
```

### Handler

```go
// internal/web/handler_phase.go

func (s *Server) handlePhaseDetail(w http.ResponseWriter, r *http.Request) {
    phaseID := r.PathValue("id")

    s.mu.RLock()
    detail, ok := s.phases[phaseID]
    n := s.nebula
    s.mu.RUnlock()

    if !ok {
        // Phase exists in nebula but no events yet — render skeleton.
        spec := findPhaseSpec(n, phaseID)
        if spec == nil {
            http.NotFound(w, r)
            return
        }
        detail = &PhaseDetail{
            ID:    spec.ID,
            Title: spec.Title,
            Status: "pending",
        }
    }

    data := PhaseDetailData{
        Phase:      detail,
        NebulaName: n.Manifest.Nebula.Name,
    }
    if err := s.templates.ExecuteTemplate(w, "phase_detail.html", data); err != nil {
        http.Error(w, "template error", http.StatusInternalServerError)
    }
}
```

### Template

```html
<!-- internal/web/templates/phase_detail.html -->
<div class="phase-header">
    <h1>{{.Phase.ID}}</h1>
    <h2>{{.Phase.Title}}</h2>
    <div class="phase-meta">
        <span class="status status--{{.Phase.Status}}">{{.Phase.Status}}</span>
        <span class="cost">${{printf "%.4f" .Phase.TotalCost}}</span>
        <span class="cycles">{{len .Phase.Cycles}} cycle(s)</span>
    </div>
</div>

<div class="cycle-timeline" id="cycle-timeline"
     hx-ext="sse" sse-connect="/events?phase={{.Phase.ID}}"
     sse-swap="cycle-update" hx-swap="beforeend">
    {{range .Phase.Cycles}}
    <div class="cycle" id="cycle-{{.Number}}">
        <div class="cycle-header">Cycle {{.Number}}</div>
        {{range .Agents}}
        <div class="agent-entry agent-entry--{{.Role}}">
            <div class="agent-header">
                <span class="role">{{.Role}}</span>
                <span class="cost">${{printf "%.4f" .CostUSD}}</span>
                <span class="duration">{{.DurationMs}}ms</span>
                {{if gt .IssueCount 0}}
                <span class="issues">{{.IssueCount}} issue(s)</span>
                {{end}}
            </div>
            <pre class="agent-output">{{.Output}}</pre>
        </div>
        {{end}}
        {{if .Summary}}
        <div class="cycle-summary">
            <span class="satisfaction satisfaction--{{.Summary.Satisfaction}}">
                {{.Summary.Satisfaction}}
            </span>
            <span class="risk risk--{{.Summary.Risk}}">risk: {{.Summary.Risk}}</span>
            <p>{{.Summary.Summary}}</p>
        </div>
        {{end}}
    </div>
    {{end}}
</div>
```

### SSE filtering

Add an optional `?phase={id}` query parameter to the `/events` endpoint so the phase detail page can subscribe to events for a single phase only, reducing noise:

```go
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
    phaseFilter := r.URL.Query().Get("phase")
    // ... in the event loop:
    if phaseFilter != "" && !eventMatchesPhase(evt, phaseFilter) {
        continue
    }
    // ...
}
```

### State accumulation

Register an internal subscriber on the `EventSource` that accumulates events into `PhaseDetail` entries:

```go
// internal/web/phase_accumulator.go

// PhaseAccumulator subscribes to the event bus and maintains
// per-phase detail state for rendering.
type PhaseAccumulator struct {
    mu     sync.RWMutex
    phases map[string]*PhaseDetail
}

func (a *PhaseAccumulator) Handle(evt Event) {
    // Switch on evt.Type, unmarshal, and update the appropriate PhaseDetail.
}
```

## Files

- `internal/web/handler_phase.go` — `handlePhaseDetail`, `PhaseDetailData`, `findPhaseSpec`
- `internal/web/handler_phase_test.go` — test rendering with varying cycle counts, empty state, and mixed agent statuses
- `internal/web/phase_state.go` — `PhaseDetail`, `CycleDetail`, `AgentDetail`, `CycleSummary` types
- `internal/web/phase_accumulator.go` — `PhaseAccumulator` that maintains per-phase state from events
- `internal/web/phase_accumulator_test.go` — test accumulation of sequential events into correct PhaseDetail shape
- `internal/web/templates/phase_detail.html` — phase detail page template with cycle timeline
- `internal/web/sse.go` — add `?phase={id}` filter support to SSE endpoint
- `internal/web/static/style.css` — add cycle timeline, agent entry, and summary styles

## Acceptance Criteria

- [ ] `GET /phase/{id}` returns an HTML page with phase header, cost, status, and cycle timeline
- [ ] Each cycle shows coder and reviewer agent entries with role, cost, duration, and issue count
- [ ] Agent output is displayed in a `<pre>` block with truncation for long outputs
- [ ] Reviewer assessment (satisfaction, risk, summary) renders when available
- [ ] Non-existent phase IDs return 404
- [ ] Pending phases (no events yet) render a skeleton page with phase metadata
- [ ] SSE endpoint accepts `?phase={id}` to filter events for a specific phase
- [ ] `PhaseAccumulator` correctly builds cycle history from sequential events
- [ ] `go test ./internal/web/...` passes
