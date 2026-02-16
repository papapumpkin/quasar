+++
id = "detail-scroll"
title = "Fix detail panel scrolling for oversized content"
type = "bug"
priority = 1
depends_on = []
+++

## Problem

The detail panel uses a `viewport.Model` for scrollable content and even renders scroll indicators ("↑ N more" / "↓ N more"), but the viewport's `Update()` method is never called from `AppModel.Update()`. This means keyboard scroll events (page up/down, arrow keys in detail mode) and mouse wheel events are never forwarded to the viewport — the content is locked to the top and can't be scrolled.

## Current State

**Detail panel** (`internal/tui/detailpanel.go`):
- `DetailPanel.Update(msg tea.Msg)` exists and calls `d.viewport.Update(msg)`
- `DetailPanel.View()` renders scroll indicators based on `linesAbove()`/`linesBelow()`
- `viewport.Model` supports: mouse wheel, page up/down, up/down arrow, home/end

**Model** (`internal/tui/model.go`):
- `AppModel.Update()` handles `tea.KeyMsg` but never forwards to `Detail.Update()`
- `AppModel.Update()` handles `spinner.TickMsg` but not viewport messages
- The detail panel is visible at `DepthAgentOutput` (loop and nebula mode)

## Solution

### 1. Forward Messages to Detail Panel

When the detail panel is visible (`showDetailPanel()` returns true), forward relevant messages to `Detail.Update()`:

```go
// In AppModel.Update(), after existing message handling:
if m.showDetailPanel() {
    m.Detail.Update(msg)
}
```

### 2. Scroll Key Bindings

Add explicit scroll key bindings that work when the detail panel is focused:
- `j`/`k` or `↑`/`↓` — scroll one line (these currently move the cursor; when at DepthAgentOutput, they should scroll instead)
- `page up`/`page down` — scroll one page
- `g`/`G` — go to top/bottom

Since `↑`/`↓` are used for cursor navigation at other depths, only remap them at `DepthAgentOutput` where the detail panel is the primary view. The viewport's built-in keybindings handle page up/down and mouse wheel.

### 3. Mouse Wheel Support

Enable mouse support in the BubbleTea program options:
```go
tea.WithMouseCellMotion() // or tea.WithMouseAllMotion()
```

This allows the viewport to handle mouse wheel scroll events natively.

## Files to Modify

- `internal/tui/model.go` — Forward messages to `Detail.Update()` when detail panel is visible; remap ↑/↓ at DepthAgentOutput to scroll
- `internal/tui/tui.go` — Add `tea.WithMouseCellMotion()` to program options
- `internal/tui/keys.go` — Add `PageUp`, `PageDown` key bindings if not present

## Acceptance Criteria

- [ ] Detail panel content scrolls with page up/page down
- [ ] Mouse wheel scrolls the detail panel when visible
- [ ] ↑/↓ scroll the detail panel at DepthAgentOutput
- [ ] Scroll indicators ("↑ N more" / "↓ N more") update as user scrolls
- [ ] Scrolling doesn't interfere with cursor navigation at other depths
- [ ] `go build` and `go test ./internal/tui/...` pass
