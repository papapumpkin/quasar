+++
id = "navigation-bar"
title = "Add navigation bar to layout for page discovery"
type = "feature"
priority = 2
depends_on = []
+++

## Problem

The layout template (`layout.html`) has no navigation. Users can navigate between pages only through in-page links (phase ID links on dashboard, back links on detail pages). The DAG page, gate list, and other routes are undiscoverable without knowing the URLs.

The TUI has a tab bar (Phases / Board / Graph / Plan / Chat) that provides clear navigation. The web dashboard should have an equivalent.

## Solution

1. **Add a `<nav>` element** to `layout.html` inside the container, above the content block. Style it as a horizontal bar matching the galactic theme.

2. **Navigation links:**
   - Dashboard (`/`) — phase table overview
   - DAG (`/dag`) — dependency graph visualization
   - Gates (`/gates`) — pending gate prompts (show count badge if pending)

3. **Active state:** Pass the current page name to the template context so the active nav link can be highlighted. Add a `CurrentPage` field to each page's data struct, or use a template variable.

4. **Style:** Match the TUI's tab bar aesthetic — active link in nebula purple with a subtle background, inactive links muted. Use the existing `--color-nebula` and `--color-surface-bright` CSS variables.

5. **Responsive:** On narrow viewports, keep nav links compact (icons + short labels).

## Files

- `internal/web/templates/layout.html` — add `<nav>` element with links
- `internal/web/static/style.css` — add nav bar styles
- `internal/web/handler_dashboard.go` — pass `CurrentPage` to template context (or use a wrapper)
- `internal/web/handler_dag.go` — pass `CurrentPage`
- `internal/web/handler_gate.go` — pass `CurrentPage`
- `internal/web/handler_phase.go` — pass `CurrentPage`
- `internal/web/handler_diff.go` — pass `CurrentPage`

## Acceptance Criteria

- [ ] All pages show a consistent navigation bar at the top
- [ ] Current page is visually highlighted in the nav
- [ ] Users can navigate to Dashboard, DAG, and Gates from any page
- [ ] Nav bar matches the galactic dark theme
- [ ] No layout shift when navigating between pages
