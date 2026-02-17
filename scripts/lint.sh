#!/usr/bin/env bash
set -euo pipefail

echo "Running golangci-lint..." >&2
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...
