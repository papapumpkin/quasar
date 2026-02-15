#!/usr/bin/env bash
set -euo pipefail

echo "Running golangci-lint..." >&2
golangci-lint run ./...
