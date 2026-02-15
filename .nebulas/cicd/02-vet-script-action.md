+++
id = "vet-script-action"
title = "Create vet script and composite action"
type = "task"
priority = 2
depends_on = []
+++

## Problem

`go vet` is run manually and inconsistently. There is no standardized script for it, and no CI integration to catch vet failures before merge.

## Solution

Create a minimal bash script `scripts/vet.sh` that runs `go vet ./...`. Wrap it in a GitHub Actions composite action for CI use.

### `scripts/vet.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "Running go vet..." >&2
go vet ./...
```

### `.github/actions/vet/action.yml`

A composite action that:

1. Checks out code via `actions/checkout@v4`
2. Sets up Go via `actions/setup-go@v5` with `go-version-file: go.mod`
3. Runs `scripts/vet.sh`

## Files to Create

- `scripts/vet.sh` — Vet runner script (must be executable)
- `.github/actions/vet/action.yml` — Composite action wrapping the script

## Acceptance Criteria

- [ ] `scripts/vet.sh` exists and is executable
- [ ] Script runs `go vet ./...`
- [ ] `.github/actions/vet/action.yml` exists as a composite action
- [ ] Action uses `go-version-file: go.mod`
- [ ] `bash scripts/vet.sh` succeeds locally
