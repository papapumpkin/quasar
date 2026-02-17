+++
id = "loop-extract-prompt-builders"
title = "Extract prompt builder functions from loop.go into prompts.go"
type = "task"
priority = 2
depends_on = ["loop-extract-findings-parser"]
scope = ["internal/loop/loop.go", "internal/loop/prompts.go"]
+++

## Problem

After extracting the findings parser (phase `loop-extract-findings-parser`), `loop.go` still contains three large prompt-building functions that are purely about string assembly:

- `buildCoderPrompt(task string, previousFindings string, workDir string) string`
- `buildRefactorPrompt(task string, previousFindings string, workDir string) string`
- `buildReviewerPrompt(task string, coderOutput string, workDir string) string`

These functions use `strings.Builder` to compose multi-paragraph prompts. They have no dependency on the `Loop` struct and are conceptually separate from loop orchestration.

## Solution

Extract into `internal/loop/prompts.go`:

1. Create `internal/loop/prompts.go`.
2. Move `buildCoderPrompt`, `buildRefactorPrompt`, and `buildReviewerPrompt` into it.
3. No signature changes needed — they are package-level functions.

## Files

- `internal/loop/loop.go` — remove prompt builder functions
- `internal/loop/prompts.go` (new) — prompt builders

## Acceptance Criteria

- [ ] `loop.go` contains only loop orchestration logic (the `Loop` struct, `Run`, `runCycle`, `handleFindings`, etc.)
- [ ] `prompts.go` contains all prompt builders
- [ ] `go test ./internal/loop/...` passes
