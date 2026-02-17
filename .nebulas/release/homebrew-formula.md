+++
id = "homebrew-formula"
title = "Homebrew formula and promote workflow"
type = "task"
priority = 2
depends_on = ["goreleaser-config"]
+++

## Problem

There is no way to distribute quasar via Homebrew.

## Solution

Create a manually-triggered promote workflow and formula template:

1. **`.github/workflows/promote-homebrew.yml`** — `workflow_dispatch` with `tag` input
   - Downloads release archives from the specified tag's GitHub release
   - Computes SHA256 checksums for each platform archive
   - Generates `quasar.rb` formula from template, substituting version/URLs/checksums
   - Pushes to `papapumpkin/homebrew-tap` repo using `HOMEBREW_TAP_TOKEN` secret

2. **`.github/homebrew/quasar.rb.tmpl`** — Homebrew formula template
   - Supports macOS ARM, macOS Intel, Linux Intel
   - `license "MIT"`
   - Test block: `assert_match "quasar", shell_output("#{bin}/quasar version")`

Archive name template in formula URLs must match `.goreleaser.yml` naming exactly.

## Files

- `.github/workflows/promote-homebrew.yml` — promote workflow
- `.github/homebrew/quasar.rb.tmpl` — formula template

## Acceptance Criteria

- [ ] Promote workflow is `workflow_dispatch` with `tag` input
- [ ] Downloads all three platform archives
- [ ] Computes SHA256 checksums
- [ ] Generates formula from template
- [ ] Pushes to `papapumpkin/homebrew-tap`
- [ ] Formula template matches goreleaser archive naming
