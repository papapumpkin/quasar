+++
id = "scratchpad-view"
title = "Scratchpad tab view"
type = "feature"
priority = 2
depends_on = ["tab-system"]
scope = ["internal/tui/scratchpadview.go", "internal/tui/scratchpadview_test.go"]
+++

## Problem

During a nebula run, workers sometimes produce observations, warnings, or notes that don't fit neatly into the phase timeline — architecture decisions, shared context, reminders. The cockpit mockup includes a "Scratchpad" tab as a shared, read-mostly viewport where these notes accumulate.

## Solution

Create a `ScratchpadView` component that renders in the `TabScratchpad` tab. It's a scrollable viewport (`bubbles/viewport`) displaying timestamped notes.

```go
type ScratchpadEntry struct {
    Timestamp time.Time
    PhaseID   string  // which phase wrote it (empty for system notes)
    Text      string
}

type ScratchpadView struct {
    entries  []ScratchpadEntry
    viewport viewport.Model
    width    int
    height   int
}
```

Each entry renders as:
```
[12:34:05] phase-id  Note text here, potentially spanning
                     multiple lines with wrapping.
```

- Timestamps in `colorMuted`
- Phase IDs in `colorAccent` (or `colorMuted` if empty/system)
- Text in `colorWhite`

**Data source**: A new `MsgScratchpadEntry` message type. Workers can emit scratchpad entries via the bridge. System events (nebula start, phase completion, errors) can also write entries automatically.

**Behavior**:
- Auto-scrolls to bottom when new entries arrive (unless user has scrolled up)
- Standard viewport keybindings: `j`/`k` or arrow keys for scrolling, `g`/`G` for top/bottom
- Read-only — no editing from the TUI (entries come from workers and system events)

The viewport wraps long lines to the terminal width. The view handles the empty state with a centered `"No notes yet"` placeholder in `colorMuted`.

## Files

- `internal/tui/scratchpadview.go` — `ScratchpadView` component with timestamped entries and scrollable viewport
- `internal/tui/scratchpadview_test.go` — Tests for entry formatting, auto-scroll behavior, and empty state

## Acceptance Criteria

- [ ] Entries render with timestamp, phase ID, and wrapped text
- [ ] Viewport scrolls with standard keybindings
- [ ] Auto-scrolls to bottom on new entries unless user has scrolled up
- [ ] Integrates with tab system (appears on `TabScratchpad`)
- [ ] New `MsgScratchpadEntry` message type defined in `msg.go`
- [ ] Empty state shows placeholder text
- [ ] `go test ./internal/tui/...` passes
- [ ] `go vet ./...` clean
