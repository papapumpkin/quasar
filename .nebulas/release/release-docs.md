+++
id = "release-docs"
title = "Create RELEASING.md"
type = "task"
priority = 3
depends_on = ["cleanup-old-build", "homebrew-formula"]
+++

## Problem

There is no documentation for the release process.

## Solution

Create `RELEASING.md` documenting:
1. Overview of tag-driven release model
2. Creating a release: choose version, tag, push, what happens automatically
3. Verifying a release: check Actions, verify assets, test download
4. Promoting to Homebrew: trigger workflow, verify `brew install`
5. One-time setup: create `papapumpkin/homebrew-tap` repo, create PAT, store as `HOMEBREW_TAP_TOKEN` secret
6. Local snapshot builds: `goreleaser build --snapshot --clean`
7. Note that conventional commits improve changelog quality

## Files

- `RELEASING.md` — release documentation

## Acceptance Criteria

- [ ] `RELEASING.md` exists
- [ ] Documents tag-driven release workflow
- [ ] Documents Homebrew promotion
- [ ] Documents one-time setup steps
- [ ] Documents local snapshot builds
