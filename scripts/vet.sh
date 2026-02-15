#!/usr/bin/env bash
set -euo pipefail

echo "Running go vet..." >&2
go vet ./...
