+++
id = "update-config-section"
title = "Update configuration section and default.yaml"
type = "task"
priority = 2
depends_on = []
scope = ["README.md", "configs/default.yaml"]
allow_scope_overlap = true
+++

## Problem

The configuration section in the README shows a `.quasar.yaml` example that may be missing newer config keys. The `configs/default.yaml` file should also be checked against the actual config loading code in `internal/config/` to ensure it includes all supported keys with accurate defaults.

Additionally, the config section doesn't mention:
- The `.quasar/` directory that Quasar creates for runtime state (fabric.db, neutrons, telemetry)
- The TUI-related config options (if any exist)

## Solution

1. Read `internal/config/config.go` (or equivalent) to identify all supported config keys and their defaults.
2. Update the `.quasar.yaml` example in the README to include any missing keys.
3. Update `configs/default.yaml` to match.
4. Add a brief note about the `.quasar/` runtime directory if it isn't mentioned.

Be conservative — only add config keys that actually exist in the config loading code. Don't invent new ones.

## Files

- `README.md` — update the Configuration section
- `configs/default.yaml` — update to match all supported keys

## Acceptance Criteria

- [ ] README config example matches all keys supported by `internal/config/`
- [ ] `configs/default.yaml` is in sync with the README example
- [ ] Default values shown are accurate
- [ ] `.quasar/` runtime directory is mentioned if applicable
