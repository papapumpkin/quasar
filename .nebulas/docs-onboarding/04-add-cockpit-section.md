+++
id = "add-cockpit-section"
title = "Add Cockpit TUI section to README"
type = "task"
priority = 2
depends_on = ["update-quickstart"]
scope = ["README.md"]
allow_scope_overlap = true
+++

## Problem

The cockpit TUI is now the default entry point when nebulas are present, but it has zero documentation in the README. A new user who runs `quasar` will see a BubbleTea terminal UI with tabs, phase tables, status bars, and animations — with no explanation of what they're looking at or how to navigate it.

The cockpit supports:
- Auto-discovery of nebulas in `.nebulas/`
- Home view with nebula listing
- Nebula execution view with phase table, status, cost tracking
- Drill-down from phases to per-phase cycle timelines to agent output
- Detail panel with plan body, diffs, bead hierarchy
- Gate prompts for human-in-the-loop review
- Real-time status bar (elapsed time, phase progress, cost, CPU/memory)
- Splash animation (skippable with `--no-splash`)
- Keyboard navigation

Key flags: `--dir`, `--no-splash`, `--max-workers`

## Solution

Add a "Cockpit (TUI)" section to the README, placed after the "Commands" section and before "Configuration". Keep it concise — this is an onboarding doc, not a full TUI manual. Cover:

1. What it is (one paragraph)
2. How to launch it (`quasar` or `quasar cockpit`)
3. Key navigation (2-3 lines about tab switching, drill-down, key bindings)
4. The `--no-tui` escape hatch for CI/piped usage
5. Mention `--no-splash` to skip the animation

Do NOT attempt to document every view, every key binding, or every message type. Just enough for a new user to orient themselves.

## Files

- `README.md` — add Cockpit section

## Acceptance Criteria

- [ ] New "Cockpit" section exists in the README
- [ ] Explains what the cockpit is and when it auto-launches
- [ ] Lists basic navigation keys
- [ ] Mentions `--no-tui` and `--no-splash` flags
- [ ] Section is concise (no more than ~30 lines)
