+++
id = "build-script-action"
title = "Create build script and composite action"
type = "task"
priority = 2
depends_on = []
+++

## Problem

There is no standardized build script that injects version metadata via ldflags. Developers build with `go build -o quasar .` but CI needs to inject the git tag/commit into the binary for release traceability.

## Solution

Create a bash script `scripts/build.sh` that builds the binary with ldflags injecting version and commit info into `cmd.Version` (`cmd/version.go:8`). The script uses `-s -w` for smaller binaries. The composite action verifies the binary works by running `./quasar version`.

### `scripts/build.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}"
LDFLAGS="-s -w -X github.com/papapumpkin/quasar/cmd.Version=${VERSION}"

echo "Building quasar ${VERSION} (${COMMIT})..." >&2
go build -ldflags "${LDFLAGS}" -o quasar .
echo "Build successful: ./quasar" >&2
```

### `.github/actions/build/action.yml`

A composite action that:

1. Checks out code via `actions/checkout@v4`
2. Sets up Go via `actions/setup-go@v5` with `go-version-file: go.mod`
3. Runs `scripts/build.sh`
4. Verifies binary with `./quasar version`

## Files to Create

- `scripts/build.sh` — Build script with ldflags (must be executable)
- `.github/actions/build/action.yml` — Composite action wrapping the script

## Acceptance Criteria

- [ ] `scripts/build.sh` exists and is executable
- [ ] Script injects version via `-X github.com/papapumpkin/quasar/cmd.Version`
- [ ] Script uses `-s -w` ldflags for smaller binaries
- [ ] VERSION and COMMIT can be overridden via environment variables
- [ ] `.github/actions/build/action.yml` runs the script and verifies with `./quasar version`
- [ ] Action uses `go-version-file: go.mod`
- [ ] `bash scripts/build.sh` succeeds locally
