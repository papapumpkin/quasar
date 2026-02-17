+++
id = "goreleaser-config"
title = "Create .goreleaser.yml"
type = "task"
priority = 1
+++

## Problem

There is no consolidated cross-compilation tooling. The existing `scripts/build.sh` handles single-target builds only, and the release workflow uses a manual matrix strategy.

## Solution

Create `.goreleaser.yml` at the project root with:
- **builds**: single entry, binary `quasar`, targets `linux/amd64`, `darwin/arm64`, `darwin/amd64`
- **ldflags**: `-s -w -X github.com/papapumpkin/quasar/cmd.Version={{.Version}}`
- **archives**: `tar.gz`, name template `quasar_{{ .Version }}_{{ .Os }}_{{ .Arch }}`
- **checksum**: sha256, filename `checksums.txt`
- **changelog**: sorted asc, grouped by conventional commit prefix (feat, fix, perf, refactor; exclude chore/ci/test/style)
- **snapshot**: `{{ incr .Patch }}-next`
- No `brews:` section — tap promotion is a separate workflow

Also add `dist/` to `.gitignore`.

## Files

- `.goreleaser.yml` — GoReleaser config
- `.gitignore` — add `dist/`

## Acceptance Criteria

- [ ] `.goreleaser.yml` exists with v2 schema
- [ ] Builds target all three platform/arch combos
- [ ] ldflags inject version into `cmd.Version`
- [ ] Archives use consistent naming template
- [ ] Changelog groups by conventional commit prefix
- [ ] `dist/` is gitignored
