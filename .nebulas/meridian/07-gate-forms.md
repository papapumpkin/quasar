+++
id = "gate-forms"
title = "Gate prompt forms for approve/reject/skip in the web UI"
type = "feature"
priority = 2
depends_on = ["sse-live-updates", "phase-detail"]
scope = ["internal/web/handler_gate.go", "internal/web/gate.go", "internal/web/templates/partials/gate_form.html"]
+++

## Problem

When a phase reaches a gate checkpoint (in `review` or `approve` mode), the TUI displays a `GatePrompt` overlay (`internal/tui/gateprompt.go`) with review summary, satisfaction score, risk level, files changed, and approve/reject/skip buttons. The response is sent back via `ResponseCh chan string` which unblocks the waiting worker. The web dashboard needs equivalent functionality: display the gate form, accept user input, and communicate the decision back to the worker.

The TUI's `Gater` (`internal/tui/gater.go`) implements the gate prompting interface. The web dashboard needs its own `WebGater` that serves gate prompts as HTML forms and collects responses via HTTP POST.

## Solution

### WebGater

Create a `WebGater` that satisfies the same interface as `tui.Gater` but serves gate prompts over HTTP:

```go
// internal/web/gate.go

// GateRequest represents a pending gate prompt waiting for a response.
type GateRequest struct {
    PhaseID       string
    PhaseTitle    string
    Satisfaction  string
    Risk          string
    Summary       string
    FilesChanged  []string
    ReviewCycles  int
    CostUSD       float64
    Options       []GateOption
    ResponseCh    chan string
    CreatedAt     time.Time
}

// GateOption mirrors tui.GateOption.
type GateOption struct {
    Label  string
    Action string
}

// WebGater manages pending gate prompts for the web UI.
// It holds a queue of gate requests and exposes them as HTML forms.
type WebGater struct {
    mu       sync.RWMutex
    pending  map[string]*GateRequest // keyed by phase ID
    server   *Server
}

// NewWebGater creates a WebGater attached to the given server.
func NewWebGater(server *Server) *WebGater {
    return &WebGater{
        pending: make(map[string]*GateRequest),
        server:  server,
    }
}

// Enqueue adds a gate prompt to the pending queue and notifies
// connected SSE clients via a gate-prompt event.
func (g *WebGater) Enqueue(req *GateRequest) {
    g.mu.Lock()
    g.pending[req.PhaseID] = req
    g.mu.Unlock()

    // Push SSE event to show the gate form in connected browsers.
    g.server.broadcastGatePrompt(req)
}

// Resolve submits a response for a pending gate prompt.
func (g *WebGater) Resolve(phaseID, action string) error {
    g.mu.Lock()
    req, ok := g.pending[phaseID]
    if !ok {
        g.mu.Unlock()
        return fmt.Errorf("no pending gate for phase %s", phaseID)
    }
    delete(g.pending, phaseID)
    g.mu.Unlock()

    req.ResponseCh <- action
    return nil
}

// Pending returns all pending gate requests.
func (g *WebGater) Pending() []*GateRequest {
    g.mu.RLock()
    defer g.mu.RUnlock()
    result := make([]*GateRequest, 0, len(g.pending))
    for _, req := range g.pending {
        result = append(result, req)
    }
    return result
}
```

### Gate form handler (GET)

Render pending gate prompts on the dashboard and on the phase detail page:

```go
// internal/web/handler_gate.go

// handleGateList renders all pending gate prompts as a list.
func (s *Server) handleGateList(w http.ResponseWriter, r *http.Request) {
    pending := s.gater.Pending()
    if err := s.templates.ExecuteTemplate(w, "gate_list.html", pending); err != nil {
        http.Error(w, "template error", http.StatusInternalServerError)
    }
}
```

### Gate response handler (POST)

```go
// handleGateResolve accepts a gate decision via form POST.
func (s *Server) handleGateResolve(w http.ResponseWriter, r *http.Request) {
    phaseID := r.PathValue("id")
    action := r.FormValue("action")

    if action == "" {
        http.Error(w, "missing action", http.StatusBadRequest)
        return
    }

    if err := s.gater.Resolve(phaseID, action); err != nil {
        http.Error(w, err.Error(), http.StatusNotFound)
        return
    }

    // Return an empty gate container to clear the form via HTMX swap.
    w.Header().Set("Content-Type", "text/html")
    fmt.Fprintf(w, `<div id="gate-%s" class="gate-resolved">Decision: %s</div>`, phaseID, action)
}
```

### Template

```html
<!-- internal/web/templates/partials/gate_form.html -->
{{define "gate-form"}}
<div id="gate-{{.PhaseID}}" class="gate-form">
    <div class="gate-header">
        <h3>Gate: {{.PhaseID}}</h3>
        <span class="gate-title">{{.PhaseTitle}}</span>
    </div>
    <div class="gate-review">
        <div class="gate-meta">
            <span class="satisfaction satisfaction--{{.Satisfaction}}">{{.Satisfaction}}</span>
            <span class="risk risk--{{.Risk}}">risk: {{.Risk}}</span>
            <span class="cost">${{printf "%.4f" .CostUSD}}</span>
            <span class="cycles">{{.ReviewCycles}} cycle(s)</span>
        </div>
        <p class="gate-summary">{{.Summary}}</p>
        {{if .FilesChanged}}
        <details class="gate-files">
            <summary>{{len .FilesChanged}} file(s) changed</summary>
            <ul>
                {{range .FilesChanged}}
                <li>{{.}}</li>
                {{end}}
            </ul>
        </details>
        {{end}}
    </div>
    <div class="gate-actions">
        {{range .Options}}
        <form hx-post="/gate/{{$.PhaseID}}" hx-target="#gate-{{$.PhaseID}}" hx-swap="outerHTML">
            <input type="hidden" name="action" value="{{.Action}}">
            <button type="submit" class="gate-btn gate-btn--{{.Action}}">{{.Label}}</button>
        </form>
        {{end}}
    </div>
</div>
{{end}}
```

### SSE integration

When a gate prompt arrives, the SSE bridge pushes a `gate-prompt` event containing the rendered gate form HTML:

```go
func (s *Server) broadcastGatePrompt(req *GateRequest) {
    var buf strings.Builder
    s.templates.ExecuteTemplate(&buf, "gate-form", req)
    s.broadcast(Event{
        Type: "gate-prompt",
        Data: buf.String(),
    })
}
```

The dashboard template has a container that receives gate prompts:

```html
<div id="gate-container" sse-swap="gate-prompt" hx-swap="beforeend">
    <!-- Gate forms appear here via SSE -->
</div>
```

### Route registration

```go
s.mux.HandleFunc("GET /gates", s.handleGateList)
s.mux.HandleFunc("POST /gate/{id}", s.handleGateResolve)
```

## Files

- `internal/web/gate.go` — `GateRequest`, `GateOption`, `WebGater`, `Enqueue`, `Resolve`, `Pending`
- `internal/web/gate_test.go` — test enqueue/resolve lifecycle, double-resolve error, concurrent access
- `internal/web/handler_gate.go` — `handleGateList`, `handleGateResolve`
- `internal/web/handler_gate_test.go` — test POST resolve returns correct HTML, missing action returns 400, unknown phase returns 404
- `internal/web/templates/partials/gate_form.html` — gate prompt form partial
- `internal/web/templates/gate_list.html` — list of all pending gate prompts
- `internal/web/sse_bridge.go` — add `broadcastGatePrompt`, `gate-prompt` event type
- `internal/web/templates/dashboard.html` — add `#gate-container` with `sse-swap="gate-prompt"`
- `internal/web/server.go` — register `/gates` and `/gate/{id}` routes, wire `WebGater` into server

## Acceptance Criteria

- [ ] Gate prompts appear as HTML forms in the dashboard when a phase hits a checkpoint
- [ ] Forms display phase ID, title, satisfaction, risk, summary, files changed, and cost
- [ ] Approve/reject/skip buttons submit via HTMX POST to `/gate/{id}`
- [ ] POST response replaces the gate form with a "Decision: {action}" confirmation
- [ ] The `ResponseCh` is written to, unblocking the waiting worker goroutine
- [ ] Multiple simultaneous gate prompts each render independently
- [ ] Double-resolving a gate returns a 404 error (already resolved)
- [ ] `GET /gates` lists all pending gate prompts
- [ ] Gate forms appear via SSE push without requiring page refresh
- [ ] `go test ./internal/web/...` passes
