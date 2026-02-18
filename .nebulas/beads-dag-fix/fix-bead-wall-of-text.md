+++
id = "fix-bead-wall-of-text"
title = "Fix beads view rendering wall of text instead of DAG tree"
type = "bug"
priority = 1
+++

## Problem

When pressing 'b' to toggle the beads view in the TUI, users see a **wall of text filling the entire screen** — some blue, some white — instead of the structured DAG tree with progress bar. This happens in all modes (loop and nebula) at all depths.

The expected output is a compact tree:
```
  Fix authentication bug  [2/5 resolved]
  ████░░░░░░░

  ├─ ✓ Missing session validation
  ├─ ✓ Token expiry edge case
  ├─ ● Race condition in refresh flow
  ├─ ● Login redirect breaks on subdomain
  └─ ● Session cleanup doesn't fire
```

## Prior Investigation & Defensive Fixes Already Applied

A thorough trace of the full rendering pipeline was performed:

**Pipeline**: `handleBeadsKey()` → `updateBeadDetail()` → `BeadView.View()` → `DetailPanel.SetContent()` → viewport

**Unit test results**: All tests pass with proper tree output at every depth level (DepthPhases, DepthPhaseLoop, DepthAgentOutput). The rendering logic is correct in isolation — `BeadView.View()` produces well-formed tree output, `DetailPanel` clips it via the viewport, and the full `View()` output stays within terminal bounds.

**Two defensive fixes were already committed:**

1. **`internal/loop/loop.go:478`** — Changed `truncate()` to `firstLine()` for bead child titles in `emitBeadUpdate()`. The old `truncate()` preserved newlines within the first 80 chars, which could break the tree layout when `ReviewFinding.Description` contained multi-line text. `firstLine()` extracts only the first line and then truncates.

2. **`internal/tui/beadview.go:103`** — Added `strings.ReplaceAll(c.Title, "\n", " ")` before truncation in `BeadView.View()` as a belt-and-suspenders defense so that even if titles arrive with newlines from any source, the tree structure is preserved.

**Likely remaining root cause (not yet fixed):**

The wall-of-text fills the **entire screen**, which suggests a layout breakage rather than just bad content. The most probable cause is a **race condition or message ordering issue** in the live TUI:

- `MsgPhaseAgentOutput` messages arrive continuously from running workers. Each one calls `updateDetailFromSelection()`, which checks `ShowBeads` and routes to `updateBeadDetail()`. However, if a `MsgPhaseAgentOutput` is processed on the **same bubbletea update tick** as the 'b' key press, the detail panel could be set to agent output AFTER `handleBeadsKey` already set it to bead content.
- In bubbletea, `Update()` processes one message at a time, so this shouldn't happen. But the returned model from `handleKey` (which uses a **value receiver** `func (m AppModel) handleKey(...)`) may not propagate mutations correctly if there's a copy issue.
- Another possibility: when `ShowBeads` is toggled ON at `DepthPhases` where the detail panel was previously hidden, the panel becomes visible but may still contain **stale agent output** from the last time it was visible at a deeper depth. The `updateBeadDetail()` call should overwrite this, but if bead data hasn't arrived yet (`m.PhaseBeads[phaseID] == nil`), it calls `SetEmpty("(no bead data for ...)")` which is a tiny message — the rest of the viewport buffer may still contain old content that lipgloss wrapping makes visible.

## Key Code Locations

- `internal/tui/model.go:970-980` — `handleBeadsKey()`: toggles `ShowBeads`, calls `updateBeadDetail()`
- `internal/tui/model.go:1085-1120` — `updateBeadDetail()`: gets bead data, calls `BeadView.View()`, sets detail panel content
- `internal/tui/model.go:1304-1309` — `updateDetailFromSelection()`: checks `ShowBeads` first before routing
- `internal/tui/model.go:289-295` — `MsgPhaseAgentOutput` handler: calls `updateDetailFromSelection()`
- `internal/tui/beadview.go:46-117` — `BeadView.View()`: renders the tree structure
- `internal/tui/detailpanel.go:52-60` — `DetailPanel.SetContent()`: wraps content, sets viewport
- `internal/tui/model.go:1461-1470` — `showDetailPanel()`: returns true when `ShowBeads` is true

## Solution

### 1. Investigate the Value Receiver Copy Issue

`handleKey` uses a value receiver `func (m AppModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd)`. It calls `m.handleBeadsKey()` which is a pointer receiver. Verify that mutations to `m.Detail` (specifically the viewport content set by `updateBeadDetail`) survive through the return path. If the `DetailPanel` viewport contains internal state that doesn't copy correctly (e.g., pointers vs values), the content update could be lost.

### 2. Force Detail Panel Reset When Beads Toggle On

In `handleBeadsKey()`, before calling `updateBeadDetail()`, explicitly clear the detail panel viewport to prevent stale content from showing:
```go
func (m *AppModel) handleBeadsKey() {
    m.ShowBeads = !m.ShowBeads
    if m.ShowBeads {
        m.ShowPlan = false
        m.ShowDiff = false
        m.DiffFileList = nil
        m.DiffFileOpen = false
        // Force-clear any stale content before populating with bead data.
        m.Detail.SetEmpty("Loading beads...")
        m.updateBeadDetail()
    }
}
```

### 3. Guard Against Stale Viewport Buffer

In `DetailPanel.SetContent()` and `SetEmpty()`, ensure the viewport's internal line buffer is fully replaced (not appended to). Verify that `viewport.SetContent("")` followed by `viewport.SetContent(newContent)` produces a clean slate.

### 4. Add a Max Children Limit

If there are many findings (50+), the tree could overflow the viewport even when rendering correctly. Add a limit:
```go
const maxBeadChildren = 30

// In BeadView.View():
if len(sorted) > maxBeadChildren {
    sorted = sorted[:maxBeadChildren]
    // Append "(and N more...)" indicator after the tree
}
```

### 5. Add Integration-Level Test

Write a test that simulates rapid `MsgPhaseAgentOutput` messages interleaved with a 'b' key press to verify the detail panel always shows bead content when `ShowBeads` is true. Verify at all three depth levels.

## Files to Modify

- `internal/tui/model.go` — Force-clear detail panel in `handleBeadsKey()` before populating beads
- `internal/tui/beadview.go` — Add max children limit with overflow indicator
- `internal/tui/detailpanel.go` — Verify viewport reset behavior on `SetContent`/`SetEmpty`
- `internal/tui/bead_debug_test.go` or new test file — Add interleaved-message integration test

## Acceptance Criteria

- [ ] Pressing 'b' shows a structured tree with connectors (`├─` / `└─`), not a wall of text
- [ ] Works at DepthPhases, DepthPhaseLoop, and DepthAgentOutput in both loop and nebula mode
- [ ] Bead view survives rapid agent output messages without content corruption
- [ ] Large numbers of children (50+) are truncated with an overflow indicator
- [ ] Status icons are color-coded: green (closed), blue (in_progress), white (open)
- [ ] `go build` and `go test ./internal/tui/... ./internal/loop/...` pass