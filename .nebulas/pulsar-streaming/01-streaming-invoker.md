+++
id = "streaming-invoker"
title = "Stream stderr from claude subprocess via goroutine and pipe"
type = "task"
priority = 1
depends_on = []
+++

## Problem

`claude.Invoker.Invoke()` buffers all stderr into a `bytes.Buffer` and blocks on `cmd.Run()`. Nothing is observable until the subprocess exits. For long-running agent invocations (minutes), the outside world has zero visibility into what the agent is doing.

## Solution

Replace the `bytes.Buffer` stderr capture with an `io.Pipe` (or `cmd.StderrPipe()`). A goroutine reads stderr line-by-line via `bufio.Scanner` and pushes each line to an optional callback. Stdout still buffers for the final JSON result — that doesn't change.

### Approach

1. Add an `OnOutput func(line string)` field to `claude.Invoker` (or accept it as a parameter). When nil, stderr is silently consumed (backward compatible).

2. In `Invoke()`:
   - Create `cmd.StderrPipe()` before starting the command
   - Use `cmd.Start()` instead of `cmd.Run()`
   - Spawn a goroutine that reads from the stderr pipe via `bufio.Scanner`, calls `OnOutput(line)` for each line, and also accumulates into a `strings.Builder` (so error messages are still available on failure)
   - `cmd.Wait()` after the goroutine completes (use a `sync.WaitGroup` or channel to coordinate)
   - Stdout remains a `bytes.Buffer` assigned to `cmd.Stdout` — no change there

3. Update tests to verify:
   - With nil `OnOutput`, behavior is identical to current
   - With a callback, lines are delivered in order
   - Stderr is still available for error reporting on failure

### Design choice: callback vs channel

Use a callback (`func(line string)`) rather than a channel. The caller (loop) will just publish to the bus in the callback body. A channel would require the caller to drain it, adding complexity. The callback runs in the stderr-reading goroutine, so it must be non-blocking — document this contract.

## Files

- `internal/claude/claude.go` — refactor `Invoke()` to use pipe + goroutine, add `OnOutput` field
- `internal/claude/claude_test.go` — add tests for streaming behavior
- `internal/agent/agent.go` — no changes needed (InvocationResult stays the same)

## Acceptance Criteria

- [ ] `Invoke()` streams stderr lines to `OnOutput` callback in real-time during execution
- [ ] With nil `OnOutput`, behavior is identical to current (no regressions)
- [ ] Stderr content is still available for error messages when invocation fails
- [ ] `cmd.Start()` + goroutine + `cmd.Wait()` pattern with proper synchronization
- [ ] Context cancellation still kills the subprocess
- [ ] `go test ./internal/claude/...` passes
- [ ] `go build ./...` and `go vet ./...` pass
