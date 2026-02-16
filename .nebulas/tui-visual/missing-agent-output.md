+++
id = "missing-agent-output"
title = "Fix coder agents sometimes showing no output/summary after completion"
type = "bug"
priority = 1
depends_on = []
+++

## Problem

Sometimes after a coder agent completes, its output/summary is empty in the TUI. The agent row shows as done (with duration and cost) but drilling into it shows no output text. This makes it impossible to understand what the coder actually did without leaving the TUI.

## Current State

**Output flow**:
1. `loop.go:runCoderPhase()` calls `l.Invoker.Invoke(ctx, ...)` which returns an `agent.Result`
2. `result.ResultText` is stored in `state.CoderOutput`
3. `l.UI.AgentDone("coder", result.CostUSD, result.DurationMs)` is called
4. `l.UI.AgentOutput("coder", state.Cycle, result.ResultText)` is called immediately after

**Bridge path** (TUI mode):
- `UIBridge.AgentDone()` sends `MsgAgentDone` (or `MsgPhaseAgentDone` for nebula)
- `UIBridge.AgentOutput()` sends `MsgAgentOutput` (or `MsgPhaseAgentOutput` for nebula)
- `model.go` handles `MsgAgentOutput` → `lv.SetAgentOutput(role, cycle, output)`
- `loopview.go:SetAgentOutput()` stores output in `AgentEntry.Output`

**Possible causes**:
1. `result.ResultText` is empty from the Claude CLI invoker — the subprocess returned no stdout or it was parsed incorrectly
2. The `MsgAgentOutput` message arrives but `SetAgentOutput` can't find the matching agent (cycle/role mismatch)
3. Race condition: `MsgAgentDone` marks the agent as done, but `MsgAgentOutput` is lost or arrives before the agent entry exists
4. In nebula mode, `MsgPhaseAgentOutput` might not find the phase's LoopView (if `ensurePhaseLoop` wasn't called yet)

## Investigation Steps

1. Check `internal/claude/invoker.go` — how is `ResultText` populated from the Claude CLI subprocess? Is stdout always captured? Could `claude -p` return output on stderr instead?
2. Check `loopview.go:SetAgentOutput()` — the matching logic searches for `role` within the cycle matching `cycle`. If the cycle number doesn't match (off-by-one), the output is silently dropped.
3. Check message ordering in the bridge — `AgentDone` is called before `AgentOutput`. Could `AgentDone` trigger a state change that makes `AgentOutput` find no matching entry?
4. In nebula mode, check whether `ensurePhaseLoop` is called before `MsgPhaseAgentOutput` arrives

## Solution

After investigation, apply the appropriate fix. Likely candidates:

### If ResultText is sometimes empty:
- Add logging in the invoker when ResultText is empty
- Check if the Claude CLI output is being truncated or lost

### If SetAgentOutput can't find the match:
- Add a fallback: if the cycle doesn't match exactly, store on the most recent agent with that role
- Log a warning when output is dropped

### If it's a race condition:
- Ensure `AgentOutput` message is sent BEFORE `AgentDone` so the entry exists by the time the output arrives
- Or buffer output in the bridge and send it as part of `AgentDone`

### If nebula PhaseLoop doesn't exist yet:
- Call `ensurePhaseLoop` in the `MsgPhaseAgentOutput` handler (currently only called in `MsgPhaseTaskStarted` and `MsgPhaseCycleStart`)

## Files to Investigate

- `internal/claude/invoker.go` — How `ResultText` is populated
- `internal/loop/loop.go` — Order of `AgentDone` / `AgentOutput` calls
- `internal/tui/loopview.go` — `SetAgentOutput` matching logic
- `internal/tui/model.go` — `MsgPhaseAgentOutput` handler
- `internal/tui/bridge.go` — `PhaseUIBridge.AgentOutput` implementation

## Acceptance Criteria

- [ ] Root cause identified and documented
- [ ] Fix applied so coder agent output is reliably shown after completion
- [ ] No output is silently dropped — if output arrives, it appears in the TUI
- [ ] Existing tests updated or new tests added to cover the fix
- [ ] `go build` and `go test ./...` pass
