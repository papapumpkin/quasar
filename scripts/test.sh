#!/usr/bin/env bash
set -euo pipefail

echo "Running tests with race detection..." >&2
go test -race -count=1 -coverprofile=coverage.out ./...

echo "Coverage summary:" >&2
go tool cover -func=coverage.out | tail -1 >&2
