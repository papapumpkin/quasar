+++
id = "sse-live-updates"
title = "Wire SSE events to HTMX swap targets for live updates"
type = "feature"
priority = 2
depends_on = ["dashboard-page"]
scope = ["internal/web/sse_bridge.go", "internal/web/templates/dashboard.html", "internal/web/templates/partials/phase_row.html", "internal/web/templates/partials/status_bar.html"]
+++

## Problem

The dashboard page from phase 2 renders a static snapshot of nebula state. As phases progress (pending -> in_progress -> done/failed), the browser shows stale data until a manual refresh. The TUI solves this with BubbleTea messages (`MsgPhaseTaskStarted`, `MsgPhaseAgentDone`, `MsgNebulaProgress`, etc. in `internal/tui/msg.go`). The web dashboard needs equivalent live updates via SSE, triggered by the Pulsar event bus and consumed by HTMX's SSE extension.

## Solution

Map event bus events to named SSE event types. Each SSE event carries an HTML fragment that HTMX swaps into the page using `hx-swap-oob="true"`. This avoids client-side JavaScript for DOM manipulation — the server renders updated HTML and pushes it.

### Event-to-SSE mapping

| Bus Event | SSE Event Name | HTML Fragment |
|-----------|---------------|---------------|
| Phase status change | `phase-update` | Updated `<tr id="phase-{id}">` row |
| Agent done (cost update) | `phase-update` | Updated row with new cost/cycles |
| Progress update | `progress-update` | Updated status bar `<div id="status-bar">` |
| Nebula done | `nebula-done` | Completion banner |
| Gate prompt | `gate-prompt` | Gate form overlay (wired in phase 7) |

### SSE bridge

```go
// internal/web/sse_bridge.go

// SSEBridge translates EventSource events into HTML fragments
// suitable for HTMX out-of-band swaps.
type SSEBridge struct {
    server *Server
}

// TranslateEvent converts a raw Event into an HTML-fragment Event
// that HTMX can swap into the DOM.
func (b *SSEBridge) TranslateEvent(evt Event) (Event, error) {
    switch evt.Type {
    case "phase-status":
        var payload PhaseStatusPayload
        if err := json.Unmarshal([]byte(evt.Data), &payload); err != nil {
            return Event{}, fmt.Errorf("unmarshal phase-status: %w", err)
        }
        // Re-render just the phase row using the template.
        html, err := b.renderPhaseRow(payload)
        if err != nil {
            return Event{}, err
        }
        return Event{Type: "phase-update", Data: html}, nil

    case "progress":
        var payload ProgressPayload
        if err := json.Unmarshal([]byte(evt.Data), &payload); err != nil {
            return Event{}, fmt.Errorf("unmarshal progress: %w", err)
        }
        html, err := b.renderStatusBar(payload)
        if err != nil {
            return Event{}, err
        }
        return Event{Type: "progress-update", Data: html}, nil
    }
    // Pass through unknown events as-is.
    return evt, nil
}
```

### HTMX client-side wiring

In the layout template, connect to the SSE endpoint:

```html
<div hx-ext="sse" sse-connect="/events">
    <!-- Phase table listens for phase-update events -->
    <table sse-swap="phase-update" hx-swap="none">
        <!-- Rows have hx-swap-oob="true" so they replace themselves -->
    </table>

    <!-- Status bar listens for progress-update events -->
    <div sse-swap="progress-update" hx-swap="innerHTML" id="status-container">
        {{template "status-bar" .}}
    </div>

    <!-- Completion overlay -->
    <div sse-swap="nebula-done" hx-swap="innerHTML" id="completion-overlay"></div>
</div>
```

### Partial templates

Extract reusable template fragments:

```go
// internal/web/templates/partials/phase_row.html
{{define "phase-row"}}
<tr id="phase-{{.ID}}" hx-swap-oob="true" class="phase-row phase-row--{{.Status}}">
    <td class="phase-status">{{.StatusIcon}}</td>
    <td><a href="/phase/{{.ID}}">{{.ID}}</a></td>
    <td>{{.Title}}</td>
    <td class="mono">${{.CostUSD}}</td>
    <td class="mono">{{.Cycles}}</td>
</tr>
{{end}}

// internal/web/templates/partials/status_bar.html
{{define "status-bar"}}
<div id="status-bar" hx-swap-oob="true" class="status-bar">
    <span class="nebula-name">{{.NebulaName}}</span>
    <div class="progress-bar">
        <div class="progress-fill" style="width: {{.ProgressPct}}%"></div>
    </div>
    <span class="progress-text">{{.Completed}}/{{.Total}}</span>
    <span class="cost mono">${{printf "%.4f" .TotalCost}}</span>
</div>
{{end}}
```

### SSE handler update

Modify the SSE handler from phase 1 to use the bridge for HTML translation:

```go
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
    // ... setup same as before ...
    bridge := &SSEBridge{server: s}

    for {
        select {
        case <-r.Context().Done():
            return
        case evt, ok := <-events:
            if !ok {
                return
            }
            translated, err := bridge.TranslateEvent(evt)
            if err != nil {
                continue // skip malformed events
            }
            fmt.Fprintf(w, "event: %s\ndata: %s\n\n", translated.Type, translated.Data)
            flusher.Flush()
        }
    }
}
```

## Files

- `internal/web/sse_bridge.go` — `SSEBridge`, `TranslateEvent`, `PhaseStatusPayload`, `ProgressPayload`, partial rendering helpers
- `internal/web/sse_bridge_test.go` — test event translation produces valid HTML fragments with correct `hx-swap-oob` attributes
- `internal/web/templates/partials/phase_row.html` — phase row partial template
- `internal/web/templates/partials/status_bar.html` — status bar partial template
- `internal/web/templates/dashboard.html` — update to include HTMX SSE extension wiring and `sse-swap` attributes
- `internal/web/templates/layout.html` — add `hx-ext="sse"` and `sse-connect="/events"` to the body wrapper
- `internal/web/sse.go` — update `handleSSE` to use the bridge for HTML translation

## Acceptance Criteria

- [ ] SSE events with type `phase-update` contain a `<tr>` fragment with `hx-swap-oob="true"` and the correct `id="phase-{id}"`
- [ ] SSE events with type `progress-update` contain an updated status bar fragment
- [ ] The dashboard page auto-connects to `/events` via HTMX SSE extension on load
- [ ] Phase rows update in real time as status changes flow through the event bus
- [ ] Progress bar updates (completed count, cost, percentage) arrive as SSE events
- [ ] Malformed events are skipped without crashing the SSE connection
- [ ] Multiple simultaneous SSE clients each receive all events independently
- [ ] `go test ./internal/web/...` passes — bridge tests verify HTML output for each event type
