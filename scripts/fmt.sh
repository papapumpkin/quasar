#!/usr/bin/env bash
set -euo pipefail

if [ $# -gt 0 ]; then
    # pre-commit passes the list of changed files as arguments
    FILES=("$@")
else
    # No arguments: format all Go files in the repo
    mapfile -t FILES < <(find . -name '*.go' -not -path './vendor/*')
fi

if [ ${#FILES[@]} -eq 0 ]; then
    echo "No Go files to format." >&2
    exit 0
fi

echo "Formatting ${#FILES[@]} Go files..." >&2
gofmt -w "${FILES[@]}"
echo "Done." >&2
