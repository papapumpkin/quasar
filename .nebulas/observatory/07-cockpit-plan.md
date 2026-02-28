+++
id = "cockpit-plan"
title = "Plan preview in cockpit home page"
type = "feature"
priority = 2
depends_on = ["plan-command", "cockpit-graph"]
scope = ["internal/tui/homeview.go", "internal/tui/planview.go"]
+++

## Problem

The cockpit home page (`internal/tui/homeview.go`) shows a scrollable list of discovered nebulas with name, phase count, status, and description. When you press Enter, it immediately launches `nebula apply` — there's no preview, no contract analysis, and no opportunity to review the execution plan before spending budget.

This is the terraform gap: `terraform apply` without `terraform plan`. Users should be able to inspect the execution graph, review contracts, and see risks *before* committing to execution.

## Solution

### 1. Two-step launch flow

Change the home page interaction from:
- **Current**: Select nebula -> Enter -> immediately apply
- **New**: Select nebula -> Enter -> plan preview -> confirm -> apply

### 2. New view: `internal/tui/planview.go`

```go
// PlanView shows the execution plan for a selected nebula before apply.
// It renders the DAG graph, contract summary, risk list, and stats,
// with action buttons to proceed or cancel.
type PlanView struct {
    Plan      *nebula.ExecutionPlan
    GraphView *GraphView        // embedded DAG visualization
    viewport  viewport.Model    // scrollable content
    selected  PlanAction        // currently highlighted action
    width     int
    height    int
}

type PlanAction int
const (
    PlanActionApply  PlanAction = iota
    PlanActionCancel
    PlanActionSave
)
```

### 3. Plan view layout

Split the view into sections (vertically scrollable):

```
Observatory: relativity                    [Apply] [Save] [Cancel]
═══════════════════════════════════════════════════════════════════

┌─ Execution Graph ───────────────────────────────────────────────┐
│                                                                 │
│  [spacetime-model] ─┬─> [spacetime-lock]                       │
│                     └─> [nebula-scanner] ─> [catalog-reports]   │
│                              ─> [agent-synthesis]               │
│                                    ─> [cli-relativity]          │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘

Contracts (8 fulfilled, 0 missing, 0 conflicts):
  spacetime-model PRODUCES: SpacetimeManifest, NebulaEntry, LoadManifest
  nebula-scanner CONSUMES: SpacetimeManifest [fulfilled], NebulaEntry [fulfilled]
  ...

Risks:
  [warn] Single track — parallelism limited to 1
  [info] spacetime-lock has no downstream consumers

Stats: 6 phases | 5 waves | 1 track | Budget: $50.00
```

### 4. Actions

- **Apply** (Enter on Apply button): Proceeds to execution, transitioning to the NebulaView with workers running
- **Save** (Enter on Save button): Writes the plan to disk as JSON, shows confirmation toast
- **Cancel** (Esc or Enter on Cancel button): Returns to home page

### 5. Message flow

```go
// New messages in msg.go:
type MsgPlanReady struct {
    Plan      *nebula.ExecutionPlan
    NebulaDir string
}

type MsgPlanAction struct {
    Action PlanAction
    Plan   *nebula.ExecutionPlan
}
```

When the user selects a nebula on the home page, instead of immediately sending `MsgNebulaInit`, the model:
1. Loads the nebula (parse + validate)
2. Runs `PlanEngine.Plan()` in a goroutine (shows spinner: "Analyzing contracts...")
3. Sends `MsgPlanReady` with the result
4. Transitions to PlanView

When the user chooses Apply, the model sends `MsgPlanAction{Action: PlanActionApply}`, which triggers the existing apply flow (bead plan, worker group, etc.).

### 6. Error rendering

If the plan has error-severity risks (missing contracts, scope conflicts), render them prominently with red styling. The Apply button shows a warning badge: `[Apply (2 risks)]`. The user can still proceed, but the risks are visible.

### 7. Diff against last run

If a previous plan exists (from a saved `.plan.json` or from the last apply), show a diff section at the bottom:

```
Changes since last plan:
  + Phase agent-synthesis added
  ~ Phase nebula-scanner: scope expanded to include cmd/
```

## Files

- `internal/tui/planview.go` — `PlanView` struct, layout, rendering, action handling
- `internal/tui/homeview.go` — Change Enter handler to launch plan instead of immediate apply
- `internal/tui/model.go` — Add `PlanView` field, handle `MsgPlanReady` and `MsgPlanAction`, add plan loading goroutine
- `internal/tui/msg.go` — Add `MsgPlanReady`, `MsgPlanAction` message types

## Acceptance Criteria

- [ ] Selecting a nebula on the home page shows a plan preview instead of immediately applying
- [ ] Plan view renders the DAG graph, contracts, risks, and stats
- [ ] Apply button launches execution (transitions to NebulaView with workers)
- [ ] Cancel button returns to home page
- [ ] Save button writes the plan as JSON and shows a confirmation toast
- [ ] Error-severity risks are visually prominent (red styling, badge on Apply button)
- [ ] A loading spinner shows while the plan is being computed
- [ ] Diff section appears when a previous plan exists
- [ ] Esc at any point in the plan view returns to home
- [ ] `go build` and `go vet ./...` pass
