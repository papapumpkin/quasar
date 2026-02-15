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
