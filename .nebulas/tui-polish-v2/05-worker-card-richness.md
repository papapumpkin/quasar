+++
id = "worker-card-richness"
title = "Richer worker cards with activity details and token counts"
type = "task"
priority = 2
depends_on = []
+++

## Problem

The worker cards for running phases show minimal information: phase ID, quasar ID, cycle count, token count, file claims, and a generic activity string ("coding...", "reviewing..."). The developer can't tell *what* the agent is actually working on without drilling into the detail panel. The token count also appears static — it only updates when `MsgPhaseAgentDone` fires, not during execution.

## Solution

Make worker cards more informative and interactive.

### Approach

1. **Richer activity line**: Instead of just "coding..." or "reviewing...", show what the agent is working on. The `WorkerCard.Activity` field exists but is only set to the generic role-based default. Populate it from:
   - The phase title (e.g., "coding: Wire streaming invoker...")
   - The current cycle's focus — if the reviewer gave feedback, show a truncated version of the feedback being addressed
   - File claims — "coding: 3 files in internal/tui/"

2. **Live token/cost updates**: The `TotalTokens` field on `StatusBar` is updated via `MsgPhaseAgentDone.Tokens` (model.go:395). This only fires *after* an agent returns. For live updates during execution:
   - Check if the loop emits any intermediate cost events
   - If not, consider adding a periodic cost check — but this may not be possible without streaming (ties into the pulsar-streaming nebula)
   - At minimum, ensure the token count updates immediately when each agent (coder/reviewer) completes within a cycle, not just at phase end

3. **Elapsed time per card**: Add a per-card elapsed timer showing how long the current phase has been running. Add a `StartedAt time.Time` field to `WorkerCard`, render it as "2m 15s" in the card.

4. **Cost per card**: Show the per-phase cost spent so far. The `MsgPhaseAgentDone` events carry `CostUSD` — accumulate per card.

5. **Cycle progress indicator**: The card already shows "cycle 2/5" but could show a mini progress bar like the status bar does. Reuse `renderCycleBar` from `statusbar.go`.

## Files

- `internal/tui/workercard.go` — add elapsed time, per-phase cost, cycle bar, richer activity
- `internal/tui/model.go` — populate new WorkerCard fields from MsgPhase* events
- `internal/tui/workercard_test.go` — update tests

## Acceptance Criteria

- [ ] Worker cards show the phase title (truncated) alongside the activity
- [ ] Per-card elapsed time displays and updates
- [ ] Per-card cost accumulates from agent done events
- [ ] Cycle progress has a visual mini-bar
- [ ] Token count updates after each agent completion (coder + reviewer), not just phase end
- [ ] `go test ./internal/tui/...` passes
- [ ] `go build ./...` and `go vet ./...` pass
