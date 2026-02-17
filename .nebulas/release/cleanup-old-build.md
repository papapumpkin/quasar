+++
id = "cleanup-old-build"
title = "Remove old build infrastructure"
type = "task"
priority = 2
depends_on = ["update-release-workflow"]
+++

## Problem

`scripts/build.sh` is now redundant since GoReleaser handles cross-compilation. The CI build action still references the old script.

## Solution

- Delete `scripts/build.sh`
- Update `.github/actions/build/action.yml` to use `goreleaser build --snapshot --clean --single-target` for CI verification builds
- Verify step uses glob `./dist/quasar_*/quasar` for binary path

Other scripts in `scripts/` remain untouched.

## Files

- `scripts/build.sh` — delete
- `.github/actions/build/action.yml` — update to use goreleaser

## Acceptance Criteria

- [ ] `scripts/build.sh` is deleted
- [ ] Build action uses `goreleaser build --snapshot --clean --single-target`
- [ ] Verify step finds binary at `./dist/quasar_*/quasar`
