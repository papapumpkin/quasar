+++
id = "activity-stream"
title = "Live activity stream on worker cards"
type = "feature"
priority = 1
depends_on = []
+++

## Problem

Worker cards currently show a static label like "coding..." or "reviewing..." with cycle count and token usage. This tells the developer *that* work is happening but not *what* is happening. When watching Claude work in the Claude Code CLI, seeing the thinking stream lets you intuit whether the agent is on the right track. The TUI needs an equivalent signal.

## Solution

Extend the worker card to show a rolling activity line — a short, continuously-updated summary of what the agent is currently doing. This is analogous to a "status line" that captures the latest tool call or reasoning step.

1. **Add an `ActivityStream` field to `WorkerCard`** — a ring buffer (last ~5 entries) of short activity strings (e.g., "reading internal/loop/run.go", "editing diffview.go:142", "running go test ./...").

2. **Emit activity events from the invoker layer.** Extend `agent.InvocationResult` or add a new streaming callback so that tool-use events (file reads, edits, bash commands) are forwarded as they happen. Define a `MsgWorkerActivity` TUI message carrying `phaseID`, `quasarID`, and `activity string`.

3. **Render the latest activity line on the worker card** beneath the existing cycle/token row. Truncate to card width. Use a dim/muted style so it doesn't compete with the phase title. Rotate through the ring buffer every ~2s if the agent hasn't sent a new event, so stale entries fade.

4. **Show a mini-progress indicator** — if the agent is in a bash command, show a spinner; if reading, show the file path; if editing, show file + line range.

## Files

- `internal/tui/workercard.go` — add `ActivityStream` field, render latest activity line
- `internal/tui/msg.go` — add `MsgWorkerActivity` message type
- `internal/tui/bridge.go` — map invoker activity events to `MsgWorkerActivity`
- `internal/agent/agent.go` — add activity callback or streaming event type
- `internal/tui/model.go` — route `MsgWorkerActivity` to the correct worker card

## Acceptance Criteria

- [ ] Worker cards display a rolling activity line showing the agent's current action
- [ ] Activity updates arrive in real time as the agent works (not batched at cycle end)
- [ ] Activity line truncates gracefully to card width without layout breakage
- [ ] Stale activity (>5s with no update) dims or shows elapsed time
- [ ] No performance regression with 8 concurrent workers streaming activity
