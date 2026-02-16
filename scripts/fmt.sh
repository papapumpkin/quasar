#!/usr/bin/env bash
set -euo pipefail

# pre-commit passes the list of changed files as arguments
FILES=("$@")

if [ ${#FILES[@]} -eq 0 ]; then
    echo "No Go files to format." >&2
    exit 0
fi

echo "Formatting Go files..." >&2
# gofmt -l -w only runs on the specific files staged for commit
gofmt -w "${FILES[@]}"
echo "Done." >&2
