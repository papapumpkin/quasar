+++
id = "consolidate-tui"
title = "Merge logo.go into banner.go in internal/tui/"
type = "task"
priority = 3
depends_on = ["delete-dead-code"]
scope = ["internal/tui/"]
+++

## Problem

`internal/tui/logo.go` is ~30 lines defining just two functions: `Logo()` and `LogoPlain()` (styled single-line Quasar text). These are part of the banner/brand rendering concern already handled by `banner.go` (~200 lines), which renders the full ASCII art banner in multiple sizes.

Having a separate file for 2 small functions is unnecessary indirection.

## Solution

1. Move the contents of `logo.go` (the `Logo()` and `LogoPlain()` functions and any associated styles) into `banner.go`. Place them after the existing banner rendering functions.

2. Merge `logo_test.go` into `banner_test.go`.

3. Delete `logo.go` and `logo_test.go`.

4. Deduplicate any shared imports between the two files.

## Files

- `internal/tui/logo.go` — delete (merge into banner.go)
- `internal/tui/logo_test.go` — delete (merge into banner_test.go)
- `internal/tui/banner.go` — absorb logo.go content
- `internal/tui/banner_test.go` — absorb logo_test.go content

## Acceptance Criteria

- [ ] `logo.go` and `logo_test.go` no longer exist
- [ ] `Logo()` and `LogoPlain()` are accessible from `banner.go`
- [ ] `go build ./internal/tui/...` succeeds
- [ ] `go test ./internal/tui/...` passes
