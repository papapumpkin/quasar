+++
id = "update-release-workflow"
title = "Replace release workflow with GoReleaser"
type = "task"
priority = 2
depends_on = ["goreleaser-config"]
+++

## Problem

The release workflow uses a custom matrix build strategy with `softprops/action-gh-release` and does not gate on CI passing.

## Solution

Replace `.github/workflows/release.yml` entirely:
- Use `goreleaser/goreleaser-action@v6`
- Gate on CI: add `ci` job that calls `ci.yml` via `workflow_call`, release `needs: ci`
- `fetch-depth: 0` for changelog generation from full git history
- Pass `GITHUB_TOKEN` env var

Also add `workflow_call:` trigger to `.github/workflows/ci.yml`.

## Files

- `.github/workflows/release.yml` — replace entirely
- `.github/workflows/ci.yml` — add `workflow_call:` trigger

## Acceptance Criteria

- [ ] Release workflow uses `goreleaser/goreleaser-action@v6`
- [ ] Release job `needs: ci`
- [ ] CI workflow has `workflow_call:` trigger
- [ ] `fetch-depth: 0` for full changelog
