+++
id = "agent-prompt"
title = "Fabric protocol injection into agent prompts"
type = "feature"
priority = 2
depends_on = ["fabric-cli", "discovery-cli"]
scope = ["internal/agent/prompt.go", "internal/agent/prompt_test.go"]
+++

## Problem

Quasars (worker agents) need to know the fabric protocol — how to read entanglements, claim files, and post discoveries. This protocol must be injected into their system prompt so they follow it during execution. Currently, agents receive a task description but no coordination protocol. Agents already use beads (via the existing `beads.Client`) for task tracking — the fabric protocol covers only fabric-specific coordination.

## Solution

Add a fabric protocol block to the system prompt builder. When a loop runs with fabric enabled, the agent's system prompt includes the protocol section.

### Protocol block

```go
const FabricProtocol = `## Fabric Protocol

You are one of several concurrent quasars working on this codebase.

BEFORE starting implementation:
  Run: quasar fabric entanglements
  Review the interfaces you must conform to (inbound) and produce (outbound).
  Do not deviate from entangled signatures.

BEFORE modifying any file:
  Run: quasar fabric claim --file <path>
  If the claim fails, STOP and post a discovery:
    quasar discovery --kind file_conflict --detail "<explanation>"

WHEN you complete your task:
  Run: quasar fabric post --from-file <path> --exports
  for every file containing exported interfaces you created or modified.

WHEN you discover an entanglement is wrong or insufficient:
  Run: quasar discovery --kind entanglement_dispute --detail "<explanation>"
  Then STOP and wait for resolution.

WHEN you cannot proceed without a product/requirements decision:
  Run: quasar discovery --kind requirements_ambiguity --detail "<question>"
  Then STOP and wait for resolution.

WHEN you encounter an unexpected issue outside your task scope:
  Run: quasar discovery --kind missing_dependency --detail "<what you need>"
  Then STOP and wait for resolution.

RULES:
  - Never modify files you haven't claimed.
  - Never change an entangled interface without posting a discovery.
  - Only STOP for genuine blockers. If you're uncertain but can write compilable code, proceed.
`
```

### Injection logic

The system prompt is built in the agent invocation path. Add a conditional block:

```go
// BuildSystemPrompt constructs the full system prompt for an agent.
func BuildSystemPrompt(taskDesc string, opts PromptOpts) string {
    var b strings.Builder
    b.WriteString(taskDesc)
    if opts.FabricEnabled {
        b.WriteString("\n\n")
        b.WriteString(FabricProtocol)
    }
    // ... existing prompt sections
    return b.String()
}

type PromptOpts struct {
    FabricEnabled bool
    TaskID        string // injected as QUASAR_TASK_ID context
}
```

### Pre-seeding context

When the task enters SCANNING and transitions to RUNNING, Tycho pre-loads relevant fabric state into the agent's initial context. This is injected as part of the task description:

```go
// PrependFabricContext adds current entanglements and claims to the task description.
func PrependFabricContext(desc string, snap fabric.FabricSnapshot, taskID string) string {
    var b strings.Builder
    b.WriteString("## Current Fabric State\n\n")
    b.WriteString(fabric.RenderSnapshot(snap))
    b.WriteString("\n\n---\n\n")
    b.WriteString(desc)
    return b.String()
}
```

This saves the agent from needing to run `quasar fabric read` as its first action.

### Integration with Loop

`Loop` gets a new optional field:
```go
type Loop struct {
    // ... existing fields
    FabricEnabled bool   // inject fabric protocol into agent prompts
    TaskID        string // for fabric context (QUASAR_TASK_ID)
}
```

When `FabricEnabled` is true, the coder prompt includes the protocol block and pre-seeded fabric state.

## Files

- `internal/agent/prompt.go` — `FabricProtocol` constant, `BuildSystemPrompt`, `PrependFabricContext`
- `internal/agent/prompt_test.go` — Tests for prompt construction with and without fabric
- `internal/loop/loop.go` — Add `FabricEnabled` and `TaskID` fields, inject protocol into coder prompts

## Acceptance Criteria

- [ ] `FabricProtocol` constant matches the design brief exactly
- [ ] `BuildSystemPrompt` appends protocol when `FabricEnabled` is true
- [ ] `PrependFabricContext` renders current entanglements and claims before the task description
- [ ] Protocol is NOT injected when `FabricEnabled` is false (backward compatible)
- [ ] `Loop` passes fabric context to the coder agent when enabled
- [ ] `go test ./internal/agent/...` passes
- [ ] `go vet ./...` clean
