+++
id = "integration-tests"
title = "End-to-end tests for prompt caching pipeline"
type = "task"
priority = 2
depends_on = ["nebula-wiring", "config-opt-in"]
+++

## Problem

The full pipeline — scanner → snapshot → agent prefix → claude args — needs integration testing to verify the context actually appears in invocations and that the opt-out paths work.

## Solution

### 1. Claude invoker test

Extend `internal/claude/claude_test.go` to verify that when `Agent.ContextPrefix` is set, the resulting CLI args contain a system prompt that starts with the context prefix.

### 2. Loop agent construction test

Test that `Loop.coderAgent()` and `Loop.reviewerAgent()` produce agents with the `ContextPrefix` populated when `Loop.ContextPrefix` is set.

### 3. Adapter test

Test that `tuiLoopAdapter` and `loopAdapter` propagate context prefix to the loops they create. This may need a mock invoker to capture the args.

### 4. Determinism regression test

Create a fixture repo, run `Scanner.Scan` 100 times in parallel, assert all outputs are identical. This catches any non-determinism from map iteration, goroutine ordering, etc.

## Files

- `internal/claude/claude_test.go` — test context prefix in args
- `internal/loop/loop_test.go` — test agent construction with context
- `internal/context/scanner_test.go` — determinism stress test

## Acceptance Criteria

- [ ] Test verifies `--system-prompt` arg contains context prefix when set
- [ ] Test verifies no context prefix in args when field is empty
- [ ] Determinism stress test passes (100 parallel runs, identical output)
- [ ] All existing tests continue to pass
