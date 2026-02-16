+++
id = "metrics-tests"
title = "Comprehensive tests for metrics collection, persistence, and status"
type = "task"
priority = 2
depends_on = ["worker-metrics", "agentmail-metrics", "metrics-persistence", "status-command"]
scope = ["internal/nebula/nebula_test.go"]
+++

## Problem

The new metrics system spans types, instrumentation, persistence, and a CLI command. It needs thorough test coverage.

## Solution

Add table-driven tests to `internal/nebula/nebula_test.go`.

### Metrics types tests

| Scenario | Expected |
|----------|----------|
| NewMetrics returns usable zero state | no nil panics |
| RecordPhaseStart + RecordPhaseComplete → duration computed | correct duration |
| RecordConflict increments TotalConflicts | count matches |
| RecordRestart increments TotalRestarts | count matches |
| RecordLockWait accumulates per-phase | sum matches |
| Snapshot returns independent copy | mutations don't affect original |
| Concurrent RecordPhaseComplete calls | no race (run with -race) |

### Persistence tests

| Scenario | Expected |
|----------|----------|
| SaveMetrics + LoadMetrics round-trip | equivalent data |
| LoadMetrics on empty directory | zero Metrics, no error |
| History capped at 10 entries | oldest entries dropped |
| Multiple saves append to history | history grows then caps |

### Worker/Coordinator instrumentation tests

| Scenario | Expected |
|----------|----------|
| WorkerGroup with nil Metrics | no panics, no recording |
| WorkerGroup with Metrics → phase complete | PhaseMetrics populated |
| ChannelCoordinator with Metrics → Lock wait | LockWaitTime recorded |
| ChannelCoordinator with nil Metrics | no panics |

### Status command tests

| Scenario | Expected |
|----------|----------|
| Status with metrics file | renders summary |
| Status without metrics file | renders state-only fallback |
| Status with --json | valid JSON to stdout |

## Files to Modify

- `internal/nebula/nebula_test.go` — Add metrics tests

## Acceptance Criteria

- [ ] All table-driven tests pass
- [ ] Race detector clean: `go test -race ./internal/nebula/...`
- [ ] Edge cases: empty metrics, zero phases, nil Metrics pointer
- [ ] `go test ./internal/nebula/... -v` shows clear test names
