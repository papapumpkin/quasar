+++
id = "docs-arch-hooks-verify"
title = "Scrub beads from documentation, architecture tests, examples, and verify the codebase is clean"
type = "task"
priority = 1
depends_on = ["drop-from-cmd-ui-config"]
scope = [
    "CLAUDE.md",
    "AGENTS.md",
    "README.md",
    "internal/arch_test/layering_test.go",
    "internal/arch_test/interfaces_test.go",
    "configs/default.yaml",
    "examples/dogfood-nebula/**",
    "examples/dogfood-tasks.txt",
    ".nebulas/tui-visual/bead-tracker.md",
    ".nebulas/nebula-lifecycle/neutron-compression.md",
]
+++

## Problem

After phases 1-3, the production code is beads-free. But the documentation, architecture tests, examples, and sample nebulas still reference beads, which will mislead readers and (in the case of arch tests) actively break when run. This phase scrubs those references and runs the full-repo verification gate so we can ship the nebula with confidence that nothing beads-related remains.

## Solution

Update documentation to describe the post-beads architecture. Remove beads from the architecture test layering DAG. Scrub example nebulas and config files. Run a full repo build + test + grep gate.

### Concrete changes

**`CLAUDE.md`:**
- Remove the entire "Nebula Authoring" subsection about `requires_beads` field in `[dependencies]` (or update the docs to reflect that this field is legacy and ignored)
- Remove any "use beads for task tracking" guidance
- Remove the example showing `requires_beads = []` in the manifest skeleton (replace with the now-shorter `[dependencies]` block that only has `requires_nebulae`)
- Add a one-line note in the "Project Structure" section that explains beads has been removed and phase tracking lives in nebula.state.toml + SQLite
- Do NOT modify other sections of CLAUDE.md (build commands, Go conventions, output rules, etc. are unchanged)

**`AGENTS.md`:**
- Remove all beads-workflow guidance (`bd create`, `bd ready`, `bd close`, `bd remember`)
- Remove the "Beads Workflow Context" section if it exists
- Update any "Common Workflows" sections that referenced `bd` commands to describe the new workflow (phase status via nebula CLI; full design in `docs/superpowers/specs/2026-06-03-quasar-autonomous-issue-to-pr-design.md`)

**`README.md`:**
- Remove beads from the installation / prerequisites section
- Remove beads-driven feature descriptions
- If the README has an architecture diagram or component list, remove beads from it

**`configs/default.yaml`:**
- Remove the `[beads]` (or `beads:`) block if present
- Remove any beads-related defaults

**`internal/arch_test/layering_test.go`:**
- Remove `"beads": 0` (or whatever layer) from the `layers` map
- Remove any test cases that asserted beads-specific layering rules
- The DAG should still be self-consistent without beads

**`internal/arch_test/interfaces_test.go`:**
- Remove any interface-placement rules that referenced the `beads.Client` interface
- Remove beads from any "expected internal packages" list (since `internal/beads/` no longer exists)

**`examples/dogfood-nebula/`:**
- Delete `10-verbose-beads.md` (phase file is purely about beads)
- Delete `08-log-bead-errors.md` (phase file is purely about beads)
- `nebula.state.toml`: this is auto-managed by the nebula engine. Leave it alone. The next time the dogfood nebula runs, state.toml will reflect the post-beads state. If the file currently contains beads-related state that would break the engine, delete the file entirely (it will be regenerated on next run).
- If `nebula.toml` in the dogfood-nebula contains `requires_beads = []`, leave it (legacy field is tolerated by the phase-2 parser change)

**`examples/dogfood-tasks.txt`:**
- Remove lines that describe beads tasks
- If the file becomes empty or trivially short, delete it

**`.nebulas/tui-visual/bead-tracker.md`:**
- Delete this phase file (it's a phase about visualizing beads in the TUI)

**`.nebulas/nebula-lifecycle/neutron-compression.md`:**
- Scrub beads references; rewrite affected sentences to describe phase tracking via nebula.state.toml instead

### Final Verification Gate

This phase is responsible for the final repo-wide verification. The agent must run and confirm:

```bash
go build ./...
go vet ./...
go test ./...
```

All three must exit 0.

Then a grep gate to confirm no production beads references remain:

```bash
# Production code: must return zero matches
grep -rn "beads\|bead_" --include="*.go" cmd/ internal/ 2>/dev/null \
  | grep -v "_test.go" \
  | grep -v "^Binary file"

# Documentation: must return zero matches in main docs
grep -n "beads\|bd remember\|bd create" CLAUDE.md AGENTS.md README.md 2>/dev/null

# Manifests: requires_beads as a key in any new manifest is forbidden;
# existing manifests with the legacy key are tolerated but should be flagged
grep -rn "requires_beads" .nebulas/*/nebula.toml 2>/dev/null
```

The first two grep commands must return no matches. The third may return matches in legacy nebulas — those are acceptable (the parser ignores the legacy field). Document this in the phase's completion report.

### Migration notes

- `.beads/` directory at the repo root (if present) is left alone. It's user/runtime state from the external CLI, not part of Quasar's source. Cleanup is the user's choice.
- The Nebula 0 nebula files themselves (this directory: `.nebulas/remove-beads/`) reference beads heavily by necessity — they're describing the removal. These are NOT scrubbed; they are the historical record of the cleanup.

## Files

- `CLAUDE.md` — remove beads guidance and `requires_beads` example
- `AGENTS.md` — remove beads workflow sections
- `README.md` — remove beads from install/feature/architecture descriptions
- `configs/default.yaml` — remove `[beads]` block if present
- `internal/arch_test/layering_test.go` — remove "beads" entry from layers map, remove beads-specific test cases
- `internal/arch_test/interfaces_test.go` — remove beads.Client interface rules, remove beads from expected-packages lists
- `examples/dogfood-nebula/10-verbose-beads.md` — delete
- `examples/dogfood-nebula/08-log-bead-errors.md` — delete
- `examples/dogfood-nebula/nebula.state.toml` — delete if it contains stale beads state (will be regenerated)
- `examples/dogfood-tasks.txt` — remove beads task lines; delete file if becomes trivially empty
- `.nebulas/tui-visual/bead-tracker.md` — delete
- `.nebulas/nebula-lifecycle/neutron-compression.md` — scrub beads references from the phase narrative

## Acceptance Criteria

- [ ] `grep -n "beads\|bd remember\|bd create" CLAUDE.md AGENTS.md README.md` returns zero matches
- [ ] `grep -rn "beads\|bead_" --include="*.go" cmd/ internal/ | grep -v "_test.go"` returns zero matches
- [ ] `internal/arch_test/layering_test.go` does not contain `"beads"` as a layer entry
- [ ] `internal/arch_test/interfaces_test.go` does not contain references to `beads.Client` interface rules
- [ ] `examples/dogfood-nebula/10-verbose-beads.md` no longer exists
- [ ] `examples/dogfood-nebula/08-log-bead-errors.md` no longer exists
- [ ] `.nebulas/tui-visual/bead-tracker.md` no longer exists
- [ ] `configs/default.yaml` has no `[beads]` block or `beads:` key
- [ ] `go build ./...` exits 0
- [ ] `go vet ./...` exits 0
- [ ] `go test ./...` exits 0 with no test failures
- [ ] `go test ./internal/arch_test/...` exits 0 (this is the canary: if arch tests were stale they would fail here)
- [ ] The nebula's own files (`.nebulas/remove-beads/*`) are intentionally not scrubbed — they describe the removal and are part of the historical record
- [ ] A completion summary is written to nebula state noting any legacy `requires_beads` keys still present in older nebula manifests (informational, not a failure)
