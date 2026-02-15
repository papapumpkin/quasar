+++
id = "add-golangci-lint"
title = "Add golangci-lint configuration for consistent code standards"
type = "task"
priority = 3
depends_on = ["handle-silent-errors", "add-godoc", "stdlib-string-helpers"]
+++

## Problem

There is no linting configuration beyond `go vet`. A `golangci-lint` config enforces consistent style, catches common bugs, and prevents regressions on the improvements made by earlier tasks in this nebula.

## Solution

Add a `.golangci.yml` at the project root with a curated set of linters appropriate for this codebase:

- `errcheck` — catch unhandled errors (reinforces task 07)
- `govet` — already used, formalize it
- `staticcheck` — advanced static analysis
- `unused` — dead code detection
- `gofmt` / `goimports` — formatting consistency
- `revive` — exported symbol doc comments (reinforces task 08)
- `ineffassign` — useless assignments
- `misspell` — typos in comments/strings

Keep the config minimal — don't enable noisy or opinionated linters that create false positives.

## Files to Create

- `.golangci.yml` — Linter configuration

## Acceptance Criteria

- [ ] `.golangci.yml` exists with the linters listed above enabled
- [ ] `golangci-lint run ./...` passes (or only reports pre-existing issues outside this nebula's scope)
- [ ] No overly strict or opinionated linters that would create noise
