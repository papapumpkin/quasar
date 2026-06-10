#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
TAILWIND="${TAILWIND_BIN:-tailwindcss}"
echo "templ generate…" >&2 ; templ generate ./internal/cockpit/...
echo "tailwind…" >&2 ; "$TAILWIND" -c internal/cockpit/tailwind.config.js -i internal/cockpit/assets/input.css -o internal/cockpit/assets/cockpit.css --minify
echo "go build -tags cockpit…" >&2 ; go build -tags cockpit -o quasar .
echo "built ./quasar with cockpit" >&2
