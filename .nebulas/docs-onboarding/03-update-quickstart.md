+++
id = "update-quickstart"
title = "Update Quick Start to reflect cockpit as default entry point"
type = "task"
priority = 1
depends_on = ["update-commands-and-flags"]
scope = ["README.md"]
allow_scope_overlap = true
+++

## Problem

The Quick Start section tells users to run `quasar run` as their first interaction. But the CLI now auto-launches the cockpit TUI when `.nebulas/` exists in the working directory. The Quick Start flow should reflect the actual default behavior a new user will encounter.

Current Quick Start:
1. `quasar validate`
2. `quasar run`
3. Type a task at the `quasar>` prompt

This is still valid for the REPL mode, but the default experience has changed. If a user has nebulas, running `quasar` (no subcommand) drops them into the cockpit. The Quick Start should guide users through both paths.

## Solution

Restructure the Quick Start into two paths:

**Path 1: Single task (REPL mode)**
1. `quasar validate` — verify dependencies
2. `quasar run` — start interactive REPL
3. Type a task at the prompt

**Path 2: Multi-task (Nebula mode)**
1. `quasar validate` — verify dependencies
2. Create a nebula directory with `nebula.toml` and phase files (point to the existing Nebula Blueprints section)
3. `quasar nebula validate .nebulas/my-nebula/` — validate
4. `quasar nebula apply .nebulas/my-nebula/ --auto` — run it
5. Or just `quasar` to launch the cockpit TUI

Also mention that running `quasar` with no subcommand in a directory with `.nebulas/` auto-launches the cockpit.

## Files

- `README.md` — rewrite the Quick Start section

## Acceptance Criteria

- [ ] Quick Start covers both REPL and Nebula paths
- [ ] Cockpit auto-launch behavior is mentioned
- [ ] Steps are numbered and concise
- [ ] The existing `quasar validate` step is preserved as step 1
