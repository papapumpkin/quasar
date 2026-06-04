# Agent Instructions

Quasar tracks phase state in each nebula's `nebula.state.toml` and in SQLite.
There is no external task tracker — phase visibility is via the CLI commands
below and (eventually) the web UI shipped by the autonomous issue-to-PR
rework. See `docs/superpowers/specs/2026-06-03-quasar-autonomous-issue-to-pr-design.md`
for the broader plan.

## Nebula Workflow

Nebula blueprints define multi-task plans as directories of `.md` files with
TOML frontmatter.

```bash
# Validate structure and dependencies
quasar nebula validate <path>

# Apply the blueprint (record phase tracking state)
quasar nebula apply <path>

# Apply and auto-execute tasks with workers
quasar nebula apply <path> --auto --max-workers 2

# Apply with file watching for in-flight editing
quasar nebula apply <path> --auto --watch

# View current state
quasar nebula show <path>
```

**State file:** `nebula.state.toml` in the nebula directory tracks phase
tracking IDs, task status, and reviewer reports. This file is auto-managed —
don't edit it manually.

**In-flight editing:** When `--watch` is active, editing a task `.md` file
while its worker is running triggers a checkpoint-resume cycle. The coder
summarizes progress, the new task description is loaded, and work continues
with full context.

**Reviewer reports:** After each task, the reviewer produces a `REPORT:`
block with satisfaction, risk, human review flag, and summary. Reports are
stored in state.

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is
NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **Run quality gates** (if code changed) — Tests, linters, builds
2. **Commit changes** — `git add` + `git commit` with descriptive message
3. **PUSH TO REMOTE** — This is MANDATORY:
   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```
4. **Clean up** — Clear stashes, prune remote branches
5. **Verify** — All changes committed AND pushed
6. **Hand off** — Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing — that leaves work stranded locally
- NEVER say "ready to push when you are" — YOU must push
- If push fails, resolve and retry until it succeeds
