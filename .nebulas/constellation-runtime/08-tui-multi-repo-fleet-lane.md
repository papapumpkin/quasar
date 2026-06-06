+++
id = "tui-multi-repo-fleet-lane"
title = "TUI fleet view: repo-grouped dashboard, sensor-drafts approval lane, one-keypress approve, live run telemetry"
type = "task"
priority = 2
depends_on = ["multi-repo-foundation", "github-sensor-produces-nebula", "constellation-runtime", "gc-engine"]
scope = [
    "internal/tui/**",
    "cmd/tui.go",
]
+++

## Problem

Today's TUI is a single-repo, single-nebula browser. The user said it's "pretty good honestly! I think it would be nice to find a way to refine it somehow." We're refining, not replacing — Bubble Tea stays, the keymap stays, the existing panels stay.

What changes is the dashboard's mental model. With multi-repo + sensor-driven seed nebulas (Phase 4) + the runtime (Phase 5), the home view needs to:
- Group nebulas by repo so a 4-repo operator can see the whole fleet at a glance
- Surface sensor-produced *awaiting-approval* nebulas as a first-class lane — these are the new ones the user has not yet looked at
- Show live constellation runs (Phase 5) with their current step
- Make the common action (approve a sensor draft, kick off the architect) a single keypress

The "user feedback submission via the TUI is completely broken" complaint from the user is addressed here by deleting the broken submission path and replacing it with the approve-flow (which is what the user actually wanted feedback for).

## Solution

### Home view layout

Three vertical lanes, each with its own scroll state, rendered side-by-side at widths ~33% each (collapses to stacked on narrow terminals):

```
┌─ Awaiting Approval (sensor drafts) ──┬─ In Flight ───────────────────────┬─ Recent ──────────────────────────┐
│ papapumpkin/quasar                   │ papapumpkin/quasar                │ papapumpkin/quasar                │
│   #142 add gh-rate-limit retry       │   nebula-7c2 ▸ coder-reviewer     │   #138 ✓ PR #441 merged           │
│   #143 fix flaky scheduler test      │     ↻ step 3/5 — reviewer running │   #137 ✗ failed (budget)          │
│                                      │                                   │                                   │
│ papapumpkin/relativity                │ papapumpkin/relativity            │ papapumpkin/relativity            │
│   #88 split coordinator state mgmt    │   (idle)                          │   #87 ✓ PR #12 merged             │
│                                      │                                   │                                   │
└──────────────────────────────────────┴───────────────────────────────────┴───────────────────────────────────┘
[a] approve   [r] reject   [d] details   [tab] switch lane   [/] filter   [g] git status   [q] quit
```

Each lane is grouped by repo, headers are sticky, and a fold-state-per-repo lets the user collapse repos they don't care about (saved to `<quasar-data-dir>/tui-state.json`).

### Data model

`internal/tui/fleet/fleet.go`:

```go
type Fleet struct {
    Repos []RepoLane
}

type RepoLane struct {
    Path         string
    DisplayName  string  // last two path components: "papapumpkin/quasar"
    AwaitingApproval []NebulaCard
    InFlight     []RunCard
    Recent       []NebulaCard
    Folded       bool
}

type NebulaCard struct {
    ID           string
    Title        string
    SourceLabel  string  // "#142" or "manual" or "scheduled"
    Status       string  // awaiting_approval, completed, failed, ...
    Age          time.Duration
    PRNumber     int     // 0 if no PR yet
    PRStatus     string  // "open" | "merged" | "closed" | ""
}

type RunCard struct {
    RunID            string
    NebulaID         string
    NebulaTitle      string
    ConstellationName string
    CurrentNode      string  // "coder" or "reviewer" etc.
    StepIndex        int
    StepCount        int
    State            string  // "running" | "paused" | "blocked_on_review"
}
```

Population:
- `AwaitingApproval`: `SELECT * FROM nebulas WHERE status='awaiting_approval' AND deleted_at IS NULL ORDER BY created_at DESC`
- `InFlight`: `SELECT ... FROM constellation_runs WHERE state IN ('running', 'paused') AND deleted_at IS NULL`
- `Recent`: last 10 terminal nebulas per repo, ordered by `completed_at DESC`

### Refresh policy

The user already said "manual refresh only, all open issues, hide forever" for the old TUI. The fleet view keeps that:
- `R` (capital) — full refresh (re-query SQLite)
- `r` (lowercase) — reject the selected card
- Background refresh ONLY for the InFlight lane, on a 2s tick — but it only refreshes that lane, not the others.

This avoids the "screen redraws while I'm scrolling" antipattern.

### Approve action (the killer feature)

When the cursor is on a `NebulaCard` in `AwaitingApproval`:

- `a` → mark the nebula `status='approved'`, enqueue a `trigger_queue` row for the `architect` constellation, refresh.
- `A` → same, but immediately page to the run details view so the user sees the architect spinning up.
- `r` → mark `status='rejected'`, write a short reason via single-line prompt, refresh.
- `d` → open detail view showing the seed prompt, source URL, sensor that produced it.

Behind the scenes this is one SQL transaction + one `trigger_queue` INSERT. The runtime's supervisor (Phase 5) picks it up on its next tick (≤1s typical).

### Run detail view

Opens on `d` from an InFlight card. Shows:
- Step-by-step trace from `star_invocations` (read-only, scrollable)
- Current step's tail of stdout/stderr (last 200 lines, polled from the runtime's log file)
- `p` pause / `P` resume the run
- `k` kill the run (sets `state='killed'`, scheduler reaps the worktree)
- `b` back to fleet view

The trace is rendered from the database, not from a live stream — the runtime persists each `star_invocation` start/end, so the TUI just polls. This is robust to runtime restarts.

### Filters & search

`/` opens a filter input. Supported filters:
- `/repo:quasar` — only that repo
- `/state:running` — only that state
- `/since:2h` — only items younger than 2h
- Plain text — substring match on title

Filters are AND'd. Clearing the filter (Esc) restores the full fleet.

### Git status panel

`g` toggles a bottom strip showing `git status --porcelain=v2` summary per repo (untracked, modified, branch ahead/behind). This is purely informational — the operator's mental model includes "do I have uncommitted changes that conflict with what Quasar is doing?"

### State persistence

`<quasar-data-dir>/tui-state.json`:
```json
{
  "folded_repos": ["papapumpkin/old-thing"],
  "active_lane": "awaiting_approval",
  "filter": ""
}
```

Saved on quit. No live writes — keeps disk noise minimal.

### Bubble Tea structure

`internal/tui/fleet/model.go` is the top-level `tea.Model`. Sub-models per lane (`internal/tui/fleet/lane_awaiting.go`, `lane_inflight.go`, `lane_recent.go`) handle their own keymaps and rendering. Messages:

- `tea.WindowSizeMsg` → re-layout
- `lane.RefreshMsg` → re-query SQLite for that lane
- `lane.TickMsg` (2s, in-flight only) → refresh in-flight
- `lane.ApproveMsg{nebulaID}` → SQL + trigger enqueue
- `lane.RejectMsg{nebulaID, reason}` → SQL update

### Deletions from this phase

- Remove the broken feedback-submission code path. The TUI directory currently has a `feedback/` sub-package the user flagged as broken; it's deleted in this phase. The "send feedback" keybinding is replaced with "approve/reject".
- Remove any leftover single-repo assumptions in the existing TUI (current repo path is hardcoded in a few places).

### Test approach

TUI tests are notoriously brittle. Strategy:
- Pure-function tests for layout (given a `Fleet`, render it to a string, compare to golden)
- Bubble Tea `teatest` for keymap behavior (a → approve)
- Integration tests using a real `:memory:` SQLite seeded with a small fixture fleet
- Golden files live in `internal/tui/fleet/testdata/`

No tests against the actual terminal — `teatest` handles that.

## Files

- `internal/tui/fleet/model.go` (new) — top-level fleet model
- `internal/tui/fleet/lane_awaiting.go` (new)
- `internal/tui/fleet/lane_inflight.go` (new)
- `internal/tui/fleet/lane_recent.go` (new)
- `internal/tui/fleet/detail.go` (new) — run detail view
- `internal/tui/fleet/filter.go` (new)
- `internal/tui/fleet/state.go` (new) — load/save tui-state.json
- `internal/tui/fleet/fleet.go` (new) — Fleet/RepoLane/NebulaCard/RunCard types + query layer
- `internal/tui/fleet/testdata/*.golden` (new)
- `internal/tui/fleet/*_test.go` (new)
- `internal/tui/feedback/` (delete) — broken; replaced by approve/reject in this phase
- `cmd/tui.go` — switch top-level model from old single-repo browser to fleet view

## Acceptance Criteria

- [ ] `quasar tui` opens the three-lane fleet view grouped by repo
- [ ] Nebulas with `status='awaiting_approval'` appear in the left lane under their repo
- [ ] Live `constellation_runs` appear in the middle lane with current node + step index
- [ ] Recently terminal nebulas appear in the right lane with PR status if any
- [ ] `a` on an awaiting-approval card: sets `status='approved'`, enqueues `architect` trigger, removes the card on next refresh
- [ ] `A` does the same and jumps to run detail view
- [ ] `r` on an awaiting card prompts for reason and sets `status='rejected'`
- [ ] `R` (capital) does a full re-query of all lanes
- [ ] In-flight lane auto-refreshes every 2s; other lanes do not auto-refresh
- [ ] `d` on an in-flight card opens detail view with step trace from `star_invocations`
- [ ] `p`/`P`/`k` in detail view pause/resume/kill the run
- [ ] `/repo:foo` filters to that repo across all lanes; Esc clears
- [ ] `g` toggles the git-status strip
- [ ] Fold/unfold state persists in `tui-state.json` across launches
- [ ] `internal/tui/feedback/` directory is deleted
- [ ] No code under `internal/tui/` imports `internal/sensors/github` or other sensor-specific packages — TUI reads only from the DB
- [ ] Golden snapshot tests pass for empty fleet, single-repo fleet, multi-repo fleet
- [ ] `teatest`-based keymap tests cover approve, reject, detail-open, kill
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all exit 0
