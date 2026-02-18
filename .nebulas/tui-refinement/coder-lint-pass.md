+++
id = "coder-lint-pass"
title = "Coders must run and fix linting issues before reviewer handoff"
type = "feature"
priority = 2
+++

## Problem

Coders currently hand off to the reviewer without running linting. This means the reviewer often catches linting issues that the coder should have fixed before submission. The coder should run `go vet ./...` (and optionally `go fmt`) as part of its workflow and fix any issues before the reviewer ever sees the code. If linting issues slip through, the reviewer should explicitly flag them for the coder to fix.

## Current State

`internal/loop/` — the coder-reviewer loop state machine
`internal/agent/` — agent types, roles, and the Invoker interface

The loop runs: coder produces code → reviewer reviews → issues found → back to coder → repeat. Linting is not part of either agent's workflow.

The coder's prompt is built in the loop package and sent to the Claude CLI. The reviewer's prompt similarly.

## Solution

### 1. Add Lint Step to Coder Workflow

After the coder finishes its coding pass, before handing off to the reviewer:

1. Run `go vet ./...` on the working directory
2. Run `go fmt ./...` to auto-fix formatting
3. If `go vet` reports issues, feed them back to the coder as additional context and have it fix them before proceeding
4. Only hand off to the reviewer once the lint pass is clean (or after a max retry count to prevent infinite loops)

### 2. Lint Check in Reviewer Prompt

Update the reviewer's prompt to explicitly include a linting check instruction:
- "Check for any linting issues (`go vet`, `go fmt`). If linting problems exist, flag them as issues for the coder to fix."
- This acts as a safety net in case the coder's lint pass missed something

### 3. Implementation

The lint step should be a distinct phase within the loop iteration:
- After coder completes → run lint commands → if issues, feed back to coder → once clean, hand to reviewer
- The lint commands should run via the same subprocess execution path (`exec.CommandContext`) used for other commands
- Lint results should be captured and included in the coder's next prompt if issues are found

### 4. Configuration

Add a config option to control the lint command(s):
- Default: `["go vet ./...", "go fmt ./..."]`
- Configurable in `.quasar.yaml` under a `lint` or `coder` section
- Allow disabling lint pass via config if needed

## Files to Modify

- `internal/loop/` — Add lint step between coder completion and reviewer handoff; add retry logic for lint failures
- `internal/agent/` or `internal/loop/` — Update coder prompt to include lint results when retrying
- `internal/loop/` — Update reviewer prompt to include linting check instruction
- `internal/config/` — Add lint command configuration option
- `.quasar.yaml` (example) — Document the lint config option

## Acceptance Criteria

- [ ] Coder runs `go vet ./...` after each coding pass before reviewer handoff
- [ ] Linting issues are fed back to the coder for fixing (up to a max retry count)
- [ ] Reviewer prompt includes instruction to flag any remaining linting issues
- [ ] Lint commands are configurable via `.quasar.yaml`
- [ ] Lint failures don't crash the loop — they're treated as issues for the coder to address
- [ ] `go build` and `go test ./internal/loop/...` pass