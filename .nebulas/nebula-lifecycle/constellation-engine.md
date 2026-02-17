+++
id = "constellation-engine"
title = "Constellation mode: post-execution review, refactor, and iteration"
type = "feature"
priority = 1
depends_on = ["lifecycle-state-machine"]
+++

## Problem

After a nebula executes, the results exist as completed bead issues with comments and a state file — but there's no structured way for a user to review what was built, understand the overall shape of the work, refactor individual phases, re-run unsatisfactory phases, and iterate until satisfied. The constellation is this interactive refinement workspace.

## Concept

A constellation is a nebula whose work has taken form. The user can see:
- What each phase produced (coder output, reviewer assessment, diffs)
- Which phases succeeded cleanly vs. needed multiple cycles vs. failed
- The reviewer's satisfaction/risk/needs_human_review assessments
- An aggregate view of what changed across the whole nebula

And can act:
- **Re-run** a phase: reset it to nursery state, optionally with updated instructions
- **Refactor** a phase: edit its description, triggering a new execution cycle
- **Add** a new phase: extend the constellation with additional work
- **Mark satisfied**: advance the phase to "accepted" so it's not flagged for review
- **Mark the nebula done**: transition to neutron when satisfied with everything

## Current State

**Post-execution state**:
- `nebula.state.toml` has per-phase status, bead IDs, costs
- `nebula status` command shows a summary table
- Reviewer reports (SATISFACTION/RISK/NEEDS_HUMAN_REVIEW) are stored in state

**No review workflow**: After execution, the user either re-runs the whole nebula or manually inspects beads. No structured iteration.

## Solution

### 1. Constellation Entry

`nebula constellation <path>` (or automatic after all phases complete) opens the constellation TUI — a specialized view mode focused on review and refinement.

The model detects constellation mode via `LifecycleStage == StageConstellation` and renders a review-focused interface instead of the execution-focused one.

### 2. Phase Review Cards

Each phase in constellation mode shows a review card:

```
  ☆ setup-models              SATISFIED: high  RISK: low
    2 cycles  $0.15  12.3s
    "Clean implementation, proper error handling."

  ⚠ auth-middleware           SATISFIED: medium  RISK: medium
    4 cycles  $0.48  45.2s    NEEDS_HUMAN_REVIEW: yes
    "Works but naming is inconsistent with project conventions."

  ✗ integration-tests          FAILED after 5 cycles
    5 cycles  $1.20  2m10s
    "Could not resolve test dependency on missing fixture."
```

Color coding:
- High satisfaction + low risk: green (accepted)
- Medium satisfaction or medium risk: yellow (review recommended)
- Low satisfaction or high risk or needs_human_review: orange (action needed)
- Failed: red

### 3. Phase Actions in Constellation

When a phase is selected, the user can:
- `enter` — drill into the phase to see full coder/reviewer output, diffs, beads
- `r` — **re-run**: reset the phase to nursery state and queue for re-execution
- `e` — **refactor**: edit the phase description (invokes the architect agent from `live-nebula-editing`)
- `a` — **accept**: mark the phase as user-accepted (no more review needed)
- `x` — **reject**: mark as needing re-work (flags it for re-run)

### 4. Aggregate Summary

At the top of the constellation view, show an aggregate summary:

```
  Constellation: auth-feature
  6 phases: 4 accepted  1 needs review  1 failed
  Total: $2.84  14 cycles  8m20s
  Files changed: 12  +340 -89
```

### 5. Iteration Loop

The constellation supports iteration:
1. User reviews phases, accepts good ones, re-runs or refactors others
2. Re-run phases go back to nursery and execute
3. When re-runs complete, the constellation updates with new results
4. User reviews again — the cycle repeats until all phases are accepted
5. When all phases are accepted, prompt: "All phases accepted. Compress to neutron?"

### 6. Constellation Persistence

The constellation state is persisted in the state file:
```toml
lifecycle_stage = "constellation"

[phases.setup-models]
status = "done"
user_accepted = true
review_satisfaction = "high"
review_risk = "low"

[phases.auth-middleware]
status = "done"
user_accepted = false
needs_human_review = true
```

## Files to Create

- `internal/nebula/constellation.go` — Constellation logic: phase review state, acceptance tracking, re-run queueing
- `internal/nebula/constellation_test.go` — Tests
- `internal/tui/constellation_view.go` — Constellation TUI view with review cards
- `cmd/constellation.go` — `nebula constellation` command

## Files to Modify

- `internal/nebula/state.go` — Add `UserAccepted`, `ReviewSatisfaction`, `ReviewRisk` to phase state
- `internal/nebula/types.go` — Add constellation-related types
- `internal/tui/model.go` — Constellation mode rendering and key handling
- `internal/tui/msg.go` — Constellation message types
- `cmd/nebula.go` — Register constellation subcommand

## Acceptance Criteria

- [ ] `nebula constellation <path>` opens the review TUI
- [ ] Phase review cards show satisfaction, risk, reviewer summary
- [ ] Users can accept, reject, re-run, or refactor individual phases
- [ ] Re-run phases execute and results update in the constellation
- [ ] Aggregate summary shows overall progress toward acceptance
- [ ] All-accepted prompt suggests neutron compression
- [ ] State file persists acceptance state
- [ ] `go build` and `go test ./...` pass
