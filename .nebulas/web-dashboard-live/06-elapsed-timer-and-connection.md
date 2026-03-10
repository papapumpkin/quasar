+++
id = "elapsed-timer-and-connection"
title = "Auto-refresh elapsed timer and SSE connection status indicator"
type = "feature"
priority = 3
depends_on = ["accumulator-dashboard-data", "phase-detail-sse"]
+++

## Problem

1. **Elapsed time is static**: The dashboard renders `ElapsedSec` once at page load. Unlike the TUI which ticks every second, the web dashboard's elapsed time never updates.
2. **No connection status**: If the SSE connection drops (network issue, server restart), the user has no indication. The TUI shows a status bar with live updates; the web UI silently goes stale.

## Solution

### Elapsed timer
Use htmx's built-in polling or a tiny inline `<script>` (no framework needed) to update the elapsed time display. Two approaches:

**Option A (pure htmx)**: Add a `/api/elapsed` endpoint that returns just the elapsed string. Use `hx-get="/api/elapsed" hx-trigger="every 1s" hx-swap="innerHTML"` on the elapsed `<span>`.

**Option B (minimal JS)**: Store `startTime` as a `data-start` attribute on the status bar. A 10-line inline script updates the text every second using `Date.now() - startTime`. This avoids an HTTP request per second.

Recommend **Option B** — it's lighter and doesn't hit the server.

### Connection status
Add a small connection indicator dot in the nav bar or status bar. Use the htmx SSE extension's built-in events:
- `htmx:sseOpen` — connected (green dot)
- `htmx:sseError` / `htmx:sseClose` — disconnected (red dot)

Add a `<span id="sse-status">` element and a small inline script that toggles its class based on these events. The htmx SSE extension emits these as standard DOM events.

## Files

- `internal/web/templates/layout.html` — add connection indicator element and inline script
- `internal/web/templates/partials/status_bar.html` — add `data-start` attribute for elapsed calculation
- `internal/web/static/style.css` — add connection dot styles (`.sse-connected`, `.sse-disconnected`)
- `internal/web/handler_dashboard.go` — pass `StartTimeUnix` to template context

## Acceptance Criteria

- [ ] Elapsed time ticks every second on the dashboard without server requests
- [ ] Connection indicator shows green when SSE is connected
- [ ] Connection indicator turns red when SSE disconnects
- [ ] Connection indicator is visible but unobtrusive (small dot in nav or status bar)
- [ ] No external JS dependencies — inline script only
