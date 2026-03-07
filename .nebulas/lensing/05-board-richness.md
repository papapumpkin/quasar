+++
id = "board-richness"
title = "Enrich board phase cards with at-a-glance status"
type = "feature"
priority = 2
depends_on = ["activity-stream", "work-summary"]
+++

## Problem

Board phase cards show the phase ID, title, and column position. To know what's actually happening inside a phase, the developer must drill into the detail panel. The board should be a dashboard where a glance tells you: which phases need attention, which are progressing well, and which are struggling.

## Solution

Add information layers to the board's phase cards without making them overwhelming.

1. **Progress indicator.** Show cycle progress as a compact fraction or mini progress bar: `[3/5]` or a 5-char bar. Color it: green (early cycles), yellow (>60% of max), red (final cycle).

2. **Health signal.** Derive a health signal from reviewer satisfaction and cycle count:
   - Green dot: reviewer satisfied or early cycles
   - Yellow dot: reviewer unsatisfied but cycles remaining
   - Red dot: final cycle and reviewer unsatisfied, or human review flagged
   - This gives instant "needs attention" signal.

3. **Cost badge.** Show cumulative cost on the card (e.g., `$0.42`). Highlight if approaching phase budget.

4. **Last activity snippet.** Show the most recent activity string from the activity stream (phase 01) as a dim subtitle on the card, so developers see what the agent is doing right now without opening worker details.

5. **Attention marker.** If a phase has unresolved hails or is at a gate prompt, show a prominent marker (e.g., red exclamation) on the card itself, not just in the status bar counters.

## Files

- `internal/tui/boardview.go` — enhance card rendering with progress, health, cost, activity, attention markers
- `internal/tui/nebulaview.go` — expose computed health signal from `PhaseEntry` data
- `internal/tui/styles.go` — add health-signal color styles (dot indicators)
- `internal/tui/model.go` — ensure board cards have access to worker activity and hail state

## Acceptance Criteria

- [ ] Phase cards show cycle progress indicator colored by urgency
- [ ] Health dot (green/yellow/red) visible on each active phase card
- [ ] Cost badge displayed on phase cards
- [ ] Latest activity snippet shown as dim subtitle on running phases
- [ ] Unresolved hails or gate prompts show attention marker on the card
- [ ] Cards remain compact — no more than 4-5 lines per card
