+++
id = "scratchpad-view"
title = "Scratchpad tab view"
type = "feature"
priority = 2
depends_on = ["tab-system"]
scope = ["internal/tui/scratchpadview.go", "internal/tui/scratchpadview_test.go"]
+++

## Problem

During a nebula epoch, quasars produce observations, post discoveries, and trigger state transitions. The telemetry stream captures all of this, but there's no live, human-readable feed in the cockpit. The mockup includes a "Scratchpad" tab as a shared viewport where telemetry-derived notes accumulate — a running log of what's happening across all quasars.

## Solution

Create a `ScratchpadView` component that renders in the `TabScratchpad` tab. It's a scrollable viewport (`bubbles/viewport`) displaying timestamped notes fed by `MsgScratchpadEntry` messages from the telemetry bridge (built in the fabric nebula's cockpit-wiring phase).

```go
type ScratchpadEntry struct {
    Timestamp time.Time
    PhaseID   string  // which phase generated this (empty for system events)
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

**Telemetry-derived entries include:**
- Discovery posted: `"discovery: requirements_ambiguity — <detail>"`
- Entanglement posted: `"entanglement: <name> published by <producer>"`
- Task state transitions: `"phase-x: running → review"`
- Hail raised: `"⚠ hail: <kind> — <detail>"`
- Filter failures: `"filter: build failed for phase-x"`

**Behavior**:
- Auto-scrolls to bottom when new entries arrive (unless user has scrolled up)
- Standard viewport keybindings: `j`/`k` or arrow keys for scrolling, `g`/`G` for top/bottom
- Read-only — entries come from the telemetry bridge, not user input

The viewport wraps long lines to the terminal width. The view handles the empty state with a centered `"No events yet"` placeholder in `colorMuted`.

## Files

- `internal/tui/scratchpadview.go` — `ScratchpadView` component with timestamped entries and scrollable viewport
- `internal/tui/scratchpadview_test.go` — Tests for entry formatting, auto-scroll behavior, and empty state

## Acceptance Criteria

- [ ] Entries render with timestamp, phase ID, and wrapped text
- [ ] Viewport scrolls with standard keybindings
- [ ] Auto-scrolls to bottom on new entries unless user has scrolled up
- [ ] Integrates with tab system (appears on `TabScratchpad`)
- [ ] Consumes `MsgScratchpadEntry` messages from the telemetry bridge
- [ ] Empty state shows placeholder text
- [ ] `go test ./internal/tui/...` passes
- [ ] `go vet ./...` clean
