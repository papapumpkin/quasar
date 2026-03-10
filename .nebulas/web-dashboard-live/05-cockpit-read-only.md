+++
id = "cockpit-read-only"
title = "Fix cockpit read-only mode to load and display nebula state"
type = "bug"
priority = 2
depends_on = ["accumulator-dashboard-data", "navigation-bar"]
+++

## Problem

`runCockpitWeb` in `cmd/tui.go` creates a web server with `ReadOnly: true` but:

1. **Never calls `SetNebula`**, so the dashboard always shows "No phases loaded".
2. Has no `EventSource` (correctly — it's read-only), so SSE is dead. But the dashboard still tries to connect to `/events`, which will hang forever or error.
3. There's no way to browse the nebula's phase structure, DAG, or previous state in this mode.

The cockpit web mode should load the nebula manifest and state from disk and render a static (but navigable) dashboard.

## Solution

1. **In `runCockpitWeb`**: After creating the server, parse the nebula from `NebulaDir` and load its state file. Call `srv.SetNebula(neb, state)` so the dashboard, DAG, and phase detail pages render with real data.

2. **Handle missing SSE gracefully**: When `Source` is nil, the SSE handler should return an appropriate response (e.g., send a single `connection-status: readonly` event then close, or return 204). The templates should not show "connecting..." spinners in read-only mode.

3. **Template adjustment**: In read-only mode, don't render the `hx-ext="sse" sse-connect="/events"` attributes on the container. Pass a `ReadOnly` flag through the template context. When `ReadOnly` is true, omit SSE attributes and show a "read-only" badge in the status bar.

4. **Gate list**: In read-only mode, the gates page should show "Read-only mode — no gate prompts available" instead of an error.

## Files

- `cmd/tui.go` — in `runCockpitWeb`, parse nebula and call `SetNebula`
- `internal/web/server.go` — expose `ReadOnly` flag for template use; add helper method to check mode
- `internal/web/sse.go` — handle nil Source gracefully (close connection or send readonly event)
- `internal/web/templates/layout.html` — conditionally omit SSE attributes when read-only
- `internal/web/handler_dashboard.go` — pass ReadOnly flag to template context
- `internal/web/handler_gate.go` — show read-only message instead of error

## Acceptance Criteria

- [ ] `quasar cockpit --web` loads nebula from disk and renders the dashboard with phases
- [ ] DAG page shows the dependency graph in read-only mode
- [ ] Phase detail pages show static accumulated state
- [ ] No SSE connection errors or hanging requests in read-only mode
- [ ] "Read-only" indicator visible in the UI
- [ ] Gate list shows appropriate message in read-only mode
