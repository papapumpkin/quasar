+++
id = "update-nebula-section"
title = "Update Nebula Blueprints section with current features"
type = "task"
priority = 2
depends_on = ["update-commands-and-flags"]
scope = ["README.md"]
allow_scope_overlap = true
+++

## Problem

The Nebula Blueprints section is mostly accurate but has some gaps:

1. **Missing frontmatter fields** — The phase file format example doesn't show `scope`, `allow_scope_overlap`, `blocks`, or `gate` fields, but the CLAUDE.md documents them. The README's phase frontmatter table should match CLAUDE.md.

2. **Missing `[execution]` fields** — The `nebula.toml` example is missing the `gate` field and the `agentmail` field (if still supported).

3. **Missing `nebula status` command** — The Nebula CLI Commands table lists `validate`, `plan`, `apply`, and `show` but is missing `status`.

4. **Config Cascade** — The config cascade section is accurate but could mention that the `gate` mode cascades the same way.

5. **State file** — The nebula state file (`nebula.state.toml` or `<name>.state.json`) is mentioned indirectly but not explicitly explained. A user running `nebula show` will see state data — they should know where it comes from.

## Solution

1. Update the phase file frontmatter table to include all fields documented in CLAUDE.md (scope, allow_scope_overlap, blocks, gate).
2. Add `gate` to the `[execution]` section example.
3. Add `nebula status` to the Nebula CLI Commands table.
4. Add a brief note about the state file: where it lives, what it tracks, and that `nebula show` reads it.
5. Keep changes minimal — don't rewrite working prose.

## Files

- `README.md` — update the Nebula Blueprints section

## Acceptance Criteria

- [ ] Phase frontmatter table matches CLAUDE.md's documented fields
- [ ] `gate` field is shown in `[execution]` example
- [ ] `nebula status` command appears in the CLI table
- [ ] State file is briefly explained
- [ ] No working prose is unnecessarily rewritten
