+++
id = "tui-lifecycle-views"
title = "TUI views for constellation review and neutron inspection"
type = "feature"
priority = 2
depends_on = ["constellation-engine", "neutron-compression"]
+++

## Problem

The TUI currently only handles the execution view (nursery/apply). Constellation mode needs a review-focused TUI, and neutron archives need an inspection view. Each lifecycle stage should have a distinct visual feel.

## Solution

### 1. Constellation TUI Mode

Add `ModeConstellation` to the TUI mode enum. The constellation view shows:

**Review cards** instead of execution rows:
```
┌─────────────────────────────────────────────────────┐
│ ✦ QUASAR  auth-feature ☆constellation  5/6 accepted│
├─────────────────────────────────────────────────────┤
│  ✓ setup-models         high/low    accepted        │
│  ⚠ auth-middleware      med/med     needs review    │
│  ✓ integration-tests    high/low    accepted        │
│  ✗ cleanup-legacy       FAILED      action needed   │
│  ✓ api-docs             high/low    accepted        │
│  ✓ final-review         high/low    accepted        │
├─────────────────────────────────────────────────────┤
│ Phase: auth-middleware                               │
│ Reviewer: "Naming inconsistent with conventions"    │
│ Cycles: 4  Cost: $0.48  Duration: 45.2s             │
├─────────────────────────────────────────────────────┤
│ a:accept  x:reject  r:re-run  e:refactor  n:neutron│
└─────────────────────────────────────────────────────┘
```

### 2. Neutron Inspection View

A read-only view for browsing neutron archives:

```
┌─────────────────────────────────────────────────────┐
│ ✦ QUASAR  auth-feature ★neutron  compressed         │
├─────────────────────────────────────────────────────┤
│ Completed: 2026-02-16  Cost: $2.84  Duration: 8m20s│
│ 6 phases: 5 done  1 failed  12 files  +340 -89     │
├─────────────────────────────────────────────────────┤
│  ✓ setup-models         Created User/Session models │
│  ✓ auth-middleware       JWT with RBAC               │
│  ✗ cleanup-legacy       Could not resolve deps      │
│  ...                                                 │
├─────────────────────────────────────────────────────┤
│ enter:detail  x:expand  q:quit                      │
└─────────────────────────────────────────────────────┘
```

### 3. Visual Differentiation

Each lifecycle stage has a distinct feel:
- **Nursery**: Active, dynamic — spinners, working indicators, live updates
- **Constellation**: Reflective — review cards, satisfaction colors, accept/reject actions
- **Neutron**: Archival — read-only, muted palette, summary-focused, compact

### 4. Stage Transition Animations

When transitioning between stages, show a brief visual:
- Nursery → Constellation: "Work complete. Entering review..." with a brief fade
- Constellation → Neutron: "Compressing..." with a progress indicator per phase

## Files to Create

- `internal/tui/constellation_view.go` — Review card rendering
- `internal/tui/neutron_view.go` — Neutron archive browser

## Files to Modify

- `internal/tui/model.go` — Add `ModeConstellation`, `ModeNeutron`; handle per-mode rendering and keys
- `internal/tui/keys.go` — Constellation keys (accept/reject/re-run/refactor/neutron)
- `internal/tui/footer.go` — Per-mode footer bindings
- `internal/tui/styles.go` — Constellation and neutron color treatments

## Acceptance Criteria

- [ ] Constellation TUI shows review cards with satisfaction/risk indicators
- [ ] Neutron TUI shows read-only archive browser
- [ ] Each lifecycle stage has a visually distinct feel
- [ ] Constellation actions (accept/reject/re-run/refactor) work from the TUI
- [ ] `go build` and `go test ./internal/tui/...` pass
