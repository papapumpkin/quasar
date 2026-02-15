+++
id = "add-missing-tests"
title = "Add test coverage for beads and config packages"
type = "task"
priority = 2
depends_on = ["context-in-beads"]
+++

## Problem

The `internal/beads` and `internal/config` packages have zero test files. Every exported function should have behavioral tests. The `beads` package wraps CLI execution, so tests should mock the command execution or test argument construction. The `config` package loads from Viper, so tests should verify defaults and overrides.

## Solution

### beads package
Test the argument-building logic in each method. Since methods call `exec.Command`, use a test helper pattern (e.g., `exec.Command` with `TestHelperProcess`) or extract argument construction into testable functions — similar to how `claude` package tests `buildArgs` and `buildEnv`.

### config package
Test `Load()` with:
- No config file (defaults)
- Environment variable overrides (`QUASAR_*`)
- Verify all default values are sensible

## Files to Create

- `internal/beads/beads_test.go` — Table-driven tests for argument construction
- `internal/config/config_test.go` — Tests for `Load()` defaults and env overrides

## Acceptance Criteria

- [ ] `internal/beads/beads_test.go` exists with tests for each public method's argument logic
- [ ] `internal/config/config_test.go` exists with tests for defaults and env overrides
- [ ] All tests use table-driven patterns with `t.Run`
- [ ] `go test ./internal/beads/... ./internal/config/...` passes
- [ ] `go test -cover ./internal/beads/...` shows meaningful coverage
