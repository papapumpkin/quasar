+++
id = "dashboard-page"
title = "Main dashboard page with phase table and status bar"
type = "feature"
priority = 2
depends_on = ["http-skeleton"]
scope = ["internal/web/templates/**", "internal/web/handler_dashboard.go"]
+++

## Problem

The HTTP skeleton serves routes but has no actual page content. The TUI's `NebulaView` renders a phase table with columns for ID, title, status, wave, cost, and cycles (see `PhaseEntry` struct in `internal/tui/nebulaview.go`). The TUI's `StatusBar` shows elapsed time, progress fraction, total cost, and budget. The web dashboard needs an equivalent HTML page that can be progressively enhanced with SSE updates in later phases.

## Solution

Create a dashboard template at `internal/web/templates/dashboard.html` rendered by Go's `html/template`. The handler reads the current `nebula.Nebula` and `nebula.State` from the web server struct (guarded by `sync.RWMutex`) and maps them into a view model for the template.

### View model

```go
// internal/web/handler_dashboard.go

// DashboardData is the template context for the main dashboard page.
type DashboardData struct {
    NebulaName  string
    Phases      []PhaseRow
    Completed   int
    Total       int
    TotalCost   float64
    BudgetUSD   float64
    ElapsedSec  int
    ProgressPct int
}

// PhaseRow mirrors tui.PhaseEntry for HTML rendering.
type PhaseRow struct {
    ID         string
    Title      string
    Status     string // "pending", "in_progress", "done", "failed", "skipped", "decomposed"
    StatusIcon string // Unicode icon matching TUI: checkmark, spinner dot, X, etc.
    CostUSD    string // formatted to 4 decimal places
    Cycles     string // "2/5" format
    Wave       int
    BlockedBy  []string
    DependsOn  []string
}
```

### Handler

The `handleDashboard` method on the web server reads `nebula` and `state` under `RLock`, passes them to `buildDashboardData`, then executes the `dashboard.html` template. On template error it returns 500.

### Template structure

The template should produce semantic HTML with HTMX attributes pre-wired for SSE updates (but the actual SSE wiring is phase 3):

```html
<!-- Each phase row has an ID for targeted SSE swaps -->
<tr id="phase-{{.ID}}" hx-swap-oob="true">
    <td class="phase-status phase-status--{{.Status}}">{{.StatusIcon}}</td>
    <td><a href="/phase/{{.ID}}">{{.ID}}</a></td>
    <td>{{.Title}}</td>
    <td>{{.CostUSD}}</td>
    <td>{{.Cycles}}</td>
</tr>
```

Status bar at the top:

```html
<div id="status-bar" class="status-bar">
    <span class="nebula-name">{{.NebulaName}}</span>
    <div class="progress-bar">
        <div class="progress-fill" style="width: {{.ProgressPct}}%"></div>
    </div>
    <span class="progress-text">{{.Completed}}/{{.Total}}</span>
    <span class="cost">${{printf "%.4f" .TotalCost}}</span>
    <span class="elapsed">{{.ElapsedSec}}s</span>
</div>
```

### CSS considerations

The `style.css` from phase 1 should include styles for:
- Dark background (`#0d1117`) to match the galactic theme
- Phase status colors: green for done, yellow for in_progress, red for failed, gray for pending
- Table styling with monospace font for IDs and costs
- Status bar with a gradient progress fill

## Files

- `internal/web/handler_dashboard.go` — `DashboardData`, `PhaseRow`, `handleDashboard`, `buildDashboardData`
- `internal/web/handler_dashboard_test.go` — test template rendering with mock nebula/state data
- `internal/web/templates/dashboard.html` — full-page HTML template with phase table and status bar
- `internal/web/templates/layout.html` — base layout with `<head>`, HTMX script tag, CSS link, SSE connection setup
- `internal/web/static/style.css` — extend with dashboard-specific styles (phase table, status bar, progress bar)

## Acceptance Criteria

- [ ] `GET /` returns a valid HTML page with a table listing all phases from the nebula
- [ ] Phase rows display ID, title, status icon, cost (formatted to 4 decimal places), and cycle count
- [ ] Status bar shows nebula name, progress fraction, total cost, and elapsed time
- [ ] Each phase row has an `id="phase-{ID}"` attribute for targeted SSE updates
- [ ] Phase IDs link to `/phase/{id}` detail page
- [ ] Template renders correctly with zero-state (all phases pending, no cost)
- [ ] Template renders correctly with mixed-state (some done, some in_progress, some failed)
- [ ] `go test ./internal/web/...` passes — handler test verifies HTML output contains expected phase data
