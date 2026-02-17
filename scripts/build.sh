#!/usr/bin/env bash
set -euo pipefail

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}"
LDFLAGS="-s -w -X github.com/papapumpkin/quasar/cmd.Version=${VERSION}"

echo "Building quasar ${VERSION} (${COMMIT})..." >&2
go build -ldflags "${LDFLAGS}" -o quasar .
echo "Build successful: ./quasar" >&2
