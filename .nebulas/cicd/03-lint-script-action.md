+++
id = "lint-script-action"
title = "Create lint script and composite action"
type = "task"
priority = 2
depends_on = []
+++

## Problem

The project has a `.golangci.yml` configuration but no script to run it and no CI integration. Linting is manual and easy to forget.

## Solution

Create a bash script `scripts/lint.sh` that runs `golangci-lint run ./...` using the existing `.golangci.yml`. The composite action installs `golangci-lint` via `golangci/golangci-lint-action@v6` before delegating to the script.

### `scripts/lint.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "Running golangci-lint..." >&2
golangci-lint run ./...
```

### `.github/actions/lint/action.yml`

A composite action that:

1. Checks out code via `actions/checkout@v4`
2. Sets up Go via `actions/setup-go@v5` with `go-version-file: go.mod`
3. Installs golangci-lint via `golangci/golangci-lint-action@v6` with `args: --version` (install only, no run)
4. Runs `scripts/lint.sh`

Note: The `golangci-lint-action` is used only for installation. The actual lint invocation happens in the script for local/CI parity.

## Files to Create

- `scripts/lint.sh` — Lint runner script (must be executable)
- `.github/actions/lint/action.yml` — Composite action wrapping the script

## Acceptance Criteria

- [ ] `scripts/lint.sh` exists and is executable
- [ ] Script runs `golangci-lint run ./...`
- [ ] `.github/actions/lint/action.yml` installs golangci-lint via `golangci/golangci-lint-action@v6`
- [ ] Action uses `go-version-file: go.mod`
- [ ] Uses the existing `.golangci.yml` (no new lint config)
- [ ] `bash scripts/lint.sh` succeeds locally (assumes golangci-lint is installed)
