+++
id = "refactor-prompt"
title = "Build refactor-focused prompt for the coder-reviewer loop"
type = "feature"
priority = 2
depends_on = ["refactor-state"]
+++

## Problem

When a user triggers a refactor, the coder-reviewer loop needs a specialized prompt that focuses on code quality improvements rather than implementing new functionality. The prompt should reference the phase's original work and guide the agent toward idiomatic cleanup.

## Solution

1. Create a refactor prompt builder (likely in `internal/loop/` or `internal/nebula/`):
   - Takes the phase's original description, the files it modified, and the git diff of its changes
   - Instructs the coder to review and improve: naming, structure, error handling, test coverage, documentation
   - Explicitly states NOT to add new features or change behavior
   - The reviewer checks that refactoring didn't break anything

2. The prompt should include:
   - The original phase description for context
   - A list of files the phase touched
   - The git diff of the phase's changes
   - Clear instructions: "Improve code quality only. Do not change behavior."

## Files

- `internal/loop/prompt.go` or `internal/nebula/worker.go` — refactor prompt builder
- `internal/loop/loop.go` — if loop needs a refactor mode flag

## Acceptance Criteria

- [ ] Refactor prompt includes original context and changed files
- [ ] Prompt explicitly forbids behavioral changes
- [ ] Reviewer validates no regressions
- [ ] `go test ./...` passes
