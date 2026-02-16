+++
id = "refactor-prompt-injection"
title = "Inject refactored context into coder prompt with REFACTOR marker"
type = "feature"
priority = 1
depends_on = ["phase-change-pipeline"]
+++

## Problem

When a user edits an in-progress phase's `.md` file, the updated description needs to be injected into the coder's next prompt in a way that clearly communicates: "the user has updated your instructions mid-flight." The coder agent needs to understand this is a human-driven course correction, not just another reviewer cycle.

## Current State

**Coder prompt** (`internal/loop/prompt.go`):
- `buildCoderPrompt(state)` constructs the coder's input from: task description, previous review findings, context goals/constraints
- The prompt has sections like `[TASK]`, `[PREVIOUS FINDINGS]`, `[CONTEXT]`
- No concept of mid-execution prompt updates or user intervention markers

**CycleState** (`internal/loop/state.go`):
- Carries `TaskDescription`, `CoderOutput`, `ReviewOutput`, `Findings`, cycle number
- No field for "this description was updated mid-run"

## Solution

### 1. Refactor State in CycleState

Add fields to track refactored context:
```go
type CycleState struct {
    // ... existing fields ...
    Refactored          bool   // true if description was updated mid-run
    OriginalDescription string // the description before refactor
    RefactorDescription string // the new description from the user
}
```

### 2. REFACTOR Prompt Section

When `state.Refactored` is true, add a prominent `[REFACTOR]` section to the coder prompt:

```
[REFACTOR — USER UPDATE]
The user has updated the task description while you were working.
The original task was:
---
<original description>
---

The UPDATED task description is:
---
<new description>
---

Important: The user is actively watching and has provided this updated
guidance based on your work so far. Prioritize the new instructions
while preserving any good progress from previous cycles.

[PREVIOUS WORK]
Your output from the last cycle:
<coder output>

Reviewer feedback:
<reviewer findings>
```

### 3. Clear Refactor Flag After Use

After the coder prompt is built with the REFACTOR section, clear `state.Refactored` so subsequent cycles use the normal prompt format (with the updated description as the new baseline).

### 4. Bead Comment

When a refactor is applied, post a bead comment documenting the change:
```
[refactor cycle N] User updated task description mid-execution.
Original: <truncated original>
Updated: <truncated new>
```

### 5. TUI Indicator

When the coder picks up the refactored context, send `MsgPhaseRefactorApplied` to the TUI. The phase row could briefly show a "⟳" or "refactored" label that fades after the cycle completes.

## Files to Modify

- `internal/loop/state.go` — Add `Refactored`, `OriginalDescription`, `RefactorDescription` fields
- `internal/loop/prompt.go` — Add `[REFACTOR]` section to coder prompt when `state.Refactored`
- `internal/loop/loop.go` — Apply refactor from channel: store original, set new description, mark `Refactored = true`; post bead comment
- `internal/tui/bridge.go` — Send `MsgPhaseRefactorApplied` when refactor is consumed
- `internal/tui/nebulaview.go` — Render refactor indicator on phase row

## Acceptance Criteria

- [ ] Coder prompt includes `[REFACTOR — USER UPDATE]` section with old and new descriptions
- [ ] Coder can understand that instructions changed and what specifically changed
- [ ] Previous cycle's work is preserved in the prompt context
- [ ] Refactor flag clears after one cycle so subsequent prompts use normal format
- [ ] Bead comment documents the refactor event
- [ ] TUI shows refactor indicator on the phase
- [ ] `go build` and `go test ./...` pass
