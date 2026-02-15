+++
id = "test-script-action"
title = "Create test script and composite action"
type = "task"
priority = 2
depends_on = []
+++

## Problem

There is no standardized way to run tests locally or in CI. Developers use ad-hoc `go test` invocations with inconsistent flags. CI needs race detection and coverage reporting, but these should also be available locally.

## Solution

Create a portable bash script `scripts/test.sh` that runs `go test` with race detection, single-count execution, and coverage profiling. Wrap it in a GitHub Actions composite action that handles Go setup before delegating to the script.

### `scripts/test.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "Running tests with race detection..." >&2
go test -race -count=1 -coverprofile=coverage.out ./...

echo "Coverage summary:" >&2
go tool cover -func=coverage.out | tail -1 >&2
```

### `.github/actions/test/action.yml`

A composite action that:

1. Checks out code via `actions/checkout@v4`
2. Sets up Go via `actions/setup-go@v5` with `go-version-file: go.mod`
3. Runs `scripts/test.sh`

## Files to Create

- `scripts/test.sh` — Test runner script (must be executable)
- `.github/actions/test/action.yml` — Composite action wrapping the script

## Acceptance Criteria

- [ ] `scripts/test.sh` exists and is executable (`chmod +x`)
- [ ] Script runs `go test -race -count=1 -coverprofile=coverage.out ./...`
- [ ] Script prints coverage summary to stderr
- [ ] `.github/actions/test/action.yml` exists with composite action using `actions/checkout@v4` and `actions/setup-go@v5`
- [ ] Action uses `go-version-file: go.mod` (not a hardcoded version)
- [ ] `bash scripts/test.sh` succeeds locally
