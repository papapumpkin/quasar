+++
id = "security-script-action"
title = "Create security scan script and composite action"
type = "task"
priority = 2
depends_on = []
+++

## Problem

There is no automated vulnerability scanning. Known vulnerabilities in dependencies could go undetected.

## Solution

Create a bash script `scripts/security.sh` that runs `govulncheck ./...` for known vulnerability detection. The composite action installs `govulncheck` before delegating to the script.

### `scripts/security.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "Running vulnerability check..." >&2
govulncheck ./...
```

### `.github/actions/security/action.yml`

A composite action that:

1. Checks out code via `actions/checkout@v4`
2. Sets up Go via `actions/setup-go@v5` with `go-version-file: go.mod`
3. Installs `govulncheck` via `go install golang.org/x/vuln/cmd/govulncheck@latest`
4. Runs `scripts/security.sh`

## Files to Create

- `scripts/security.sh` — Security scan script (must be executable)
- `.github/actions/security/action.yml` — Composite action wrapping the script

## Acceptance Criteria

- [ ] `scripts/security.sh` exists and is executable
- [ ] Script runs `govulncheck ./...`
- [ ] `.github/actions/security/action.yml` installs govulncheck and runs the script
- [ ] Action uses `go-version-file: go.mod`
- [ ] `bash scripts/security.sh` succeeds locally (assumes govulncheck is installed)
