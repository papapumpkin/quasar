#!/usr/bin/env bash
set -euo pipefail

echo "Running vulnerability check..." >&2
govulncheck ./...
