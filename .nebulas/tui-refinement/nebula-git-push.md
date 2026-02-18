+++
id = "nebula-git-push"
title = "Push branch and checkout main after nebula completion"
type = "feature"
priority = 2
+++

## Problem

When a nebula finishes, it leaves the user on the nebula's working branch with unpushed commits. The user must manually run `git push --set-upstream origin <branch>` (since nebula branches typically have no upstream yet) and then `git checkout main`. This should happen automatically as part of nebula completion.

## Current State

The nebula execution flow ends with all phases completed/failed and a completion overlay shown. There is no post-completion git workflow — the TUI just displays results and waits for 'q' to exit.

`internal/nebula/` — orchestration logic that runs phases
`internal/tui/overlay.go` — completion overlay shown when done
`cmd/` — CLI commands that start the nebula

The git branching strategy uses `nebula/<name>` branches. These branches are created locally and typically have no upstream tracking branch set, so a plain `git push` fails.

## Solution

### 1. Post-Completion Git Push

After all phases are done (success or partial — at least some phases completed), automatically:

1. Stage and commit any remaining changes (if uncommitted work exists)
2. Push with `--set-upstream origin <current-branch>` to handle branches with no upstream
3. If push fails (e.g., no remote, auth failure), log the error but don't block the TUI — show the error in the completion overlay

### 2. Checkout Main on Exit

After the push succeeds (or is skipped due to error), checkout `main`:

1. Run `git checkout main`
2. If checkout fails (dirty working tree, etc.), log the error in the completion overlay

### 3. Show Status in Completion Overlay

The completion overlay should show the git status:
- "Pushed to origin/nebula/my-nebula" (success)
- "Checked out main" (success)
- Or error messages if either step failed

### 4. Implementation Location

This logic belongs in the nebula orchestration layer (not the TUI directly):
- Add a post-completion hook or step in the nebula runner
- The TUI receives a message with the git push/checkout results to display

## Files to Modify

- `internal/nebula/` — Add post-completion git push and checkout logic after all phases finish
- `internal/tui/overlay.go` — Display push/checkout status in the completion overlay
- `internal/tui/model.go` — Handle git result messages and update overlay content

## Acceptance Criteria

- [ ] After nebula completion, the branch is pushed to origin with `--set-upstream` if no upstream exists
- [ ] After push, `git checkout main` is run automatically
- [ ] Push/checkout results (success or error) shown in the completion overlay
- [ ] If push fails, the error is shown but doesn't crash the TUI
- [ ] If checkout fails, the error is shown but doesn't crash the TUI
- [ ] `go build` and `go vet ./...` pass