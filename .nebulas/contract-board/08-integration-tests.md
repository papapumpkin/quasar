+++
id = "integration-tests"
title = "End-to-end tests for contract-board pipeline"
type = "task"
priority = 2
depends_on = ["worker-integration"]
scope = ["internal/board/integration_test.go", "internal/nebula/worker_board_test.go"]
+++

## Problem

Each component (board store, publisher, poller, pushback handler) has unit tests, but the full pipeline — phase completes, contracts published, blocked phase re-polled, auto-resumes — needs end-to-end verification. Edge cases like concurrent completions, cascading unblocks, and escalation paths need integration coverage.

## Solution

Write integration tests that exercise the full contract-board pipeline with mock phases and a real SQLite board:

### Test Scenarios

**1. Happy path: linear dependency chain**
- Phase A has no deps, runs and publishes contracts
- Phase B depends on A, polls board, finds contracts, proceeds
- Verify: B receives A's contracts in its board snapshot

**2. Parallel roots post to board**
- Phases A and B have no deps, run concurrently
- Phase C depends on both A and B
- A completes first, C polls — NEED_INFO (missing B's contracts)
- B completes, C re-polls — PROCEED
- Verify: cascading resolution works, C sees both contract sets

**3. Pushback with auto-retry**
- Phase A runs, Phase B depends on A
- B polls before A completes — NEED_INFO
- A completes, B auto-re-polls — PROCEED
- Verify: retry count, blocked tracking, automatic resume

**4. Pushback with escalation**
- Phase B needs a contract that no in-progress phase can provide
- After MaxRetries, escalation signal is produced
- Verify: escalation message, gate signal compatibility

**5. File conflict detection**
- Phase A claims `internal/api/routes.go`
- Phase B (parallel, no DAG dep) tries to claim same file
- B polls — CONFLICT
- A completes, releases claims, B re-polls — PROCEED
- Verify: file claim lifecycle

**6. Contradictory contracts**
- Phase A publishes `type Store interface { Get() string }`
- Phase B publishes `type Store interface { Get() (string, error) }`
- Phase C depends on both, polls — CONFLICT (ambiguous Store)
- Verify: immediate escalation, no auto-retry

**7. Board=nil backward compatibility**
- Run WorkerGroup with Board=nil
- Verify: identical behavior to current dispatch, no panics

### Test Infrastructure

Use a real SQLite board (in-memory or temp file) with mock `agent.Invoker` for the LLMPoller. Create fixture phase specs with known dependency structures. The mock invoker returns canned PROCEED/NEED_INFO/CONFLICT responses based on the phase ID.

## Files

- `internal/board/integration_test.go` — Board-level integration tests (publisher + poller + pushback)
- `internal/nebula/worker_board_test.go` — WorkerGroup dispatch tests with board enabled

## Acceptance Criteria

- [ ] All 7 test scenarios pass
- [ ] Tests use real SQLite (in-memory) for the board, not mocks
- [ ] Mock Invoker returns deterministic poll responses
- [ ] Tests verify contract content, not just pass/fail
- [ ] Concurrent test scenarios use `t.Parallel()` where safe
- [ ] No test flakiness from timing — use channels/synchronization, not sleeps
- [ ] `go test ./internal/board/... ./internal/nebula/...` passes
- [ ] `go test -race ./internal/board/... ./internal/nebula/...` passes
