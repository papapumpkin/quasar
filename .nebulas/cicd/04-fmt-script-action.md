+++
id = "fmt-script-action"
title = "Create fmt check script and composite action"
type = "task"
priority = 2
depends_on = []
+++

## Problem

There is no automated check for Go formatting. Unformatted code can be committed without detection.

## Solution

Create a bash script `scripts/fmt.sh` that uses `gofmt -l .` to list unformatted files. If any are found, the script prints them to stderr and exits non-zero. Wrap it in a composite action for CI.

### `scripts/fmt.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "Checking formatting..." >&2
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
    echo "The following files need formatting:" >&2
    echo "$unformatted" >&2
    exit 1
fi
echo "All files formatted correctly." >&2
```

### `.github/actions/fmt/action.yml`

A composite action that:

1. Checks out code via `actions/checkout@v4`
2. Sets up Go via `actions/setup-go@v5` with `go-version-file: go.mod`
3. Runs `scripts/fmt.sh`

## Files to Create

- `scripts/fmt.sh` — Format check script (must be executable)
- `.github/actions/fmt/action.yml` — Composite action wrapping the script

## Acceptance Criteria

- [ ] `scripts/fmt.sh` exists and is executable
- [ ] Script uses `gofmt -l .` and fails if any files need formatting
- [ ] Unformatted file names are printed to stderr
- [ ] `.github/actions/fmt/action.yml` exists as a composite action
- [ ] Action uses `go-version-file: go.mod`
- [ ] `bash scripts/fmt.sh` succeeds locally (assuming code is formatted)
