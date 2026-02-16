#!/usr/bin/env bash
set -euo pipefail

echo "Formatting Go files..." >&2
changed=$(gofmt -l .)
if [ -n "$changed" ]; then
    echo "Reformatting:" >&2
    echo "$changed" >&2
    gofmt -w .
    echo "Done." >&2
else
    echo "All files already formatted." >&2
fi
