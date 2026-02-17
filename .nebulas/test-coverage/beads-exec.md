+++
id = "beads-exec"
title = "Test beads client execution paths"
type = "task"
priority = 2
+++

## Goal

Increase `internal/beads` coverage from 45.3% to ~75% by testing the execution-path methods.

## Current State

Argument-building functions are all at 100%:
- `buildQuickCreateArgs`, `buildCreateArgs`, `buildShowArgs`, `buildUpdateArgs`, `buildCloseArgs`, `buildAddCommentArgs`

Execution methods are all at 0%:
- `QuickCreate`, `Create`, `Show`, `Update`, `Close`, `AddComment`, `Validate`
- `run` (private helper that calls `exec.CommandContext`)

## Approach

1. Examine the `CLI` struct and `run` method to understand how `exec.CommandContext` is called.
2. Strategy options (pick based on what's practical):
   a. **exec.Command test helper pattern**: Use Go's standard `TestHelperProcess` pattern where the test binary re-invokes itself as a fake subprocess.
   b. **Interface extraction**: If `run` can be made injectable (e.g., a `runner` function field on the struct), mock it in tests.
   c. **Minimal integration tests**: If the `bd` binary is available in CI, test with real calls (less ideal).
3. Test each execution method verifying it passes correct args and handles stdout/stderr/errors correctly.

## Files

- `internal/beads/beads_test.go` — extend with exec tests
- `internal/beads/beads.go` — read, minimal modifications only if needed

## Scope

- `internal/beads/` only
