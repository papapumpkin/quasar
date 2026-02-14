# Agent Instructions

This project uses **bd** (beads) for issue tracking. Run `bd onboard` to get started.

## Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --status in_progress  # Claim work
bd close <id>         # Complete work
bd sync               # Sync with git
```

## Nebula Workflow

Nebula blueprints define multi-task plans as directories of `.md` files with TOML frontmatter.

```bash
# Validate structure and dependencies
quasar nebula validate <path>

# Preview what beads would be created/updated
quasar nebula plan <path>

# Apply the blueprint (create beads)
quasar nebula apply <path>

# Apply and auto-execute tasks with workers
quasar nebula apply <path> --auto --max-workers 2

# Apply with file watching for in-flight editing
quasar nebula apply <path> --auto --watch

# View current state
quasar nebula show <path>
```

**State file:** `nebula.state.toml` in the nebula directory tracks bead IDs, task status, and reviewer reports. This file is auto-managed — don't edit it manually.

**In-flight editing:** When `--watch` is active, editing a task `.md` file while its worker is running triggers a checkpoint-resume cycle. The coder summarizes progress, the new task description is loaded, and work continues with full context.

**Reviewer reports:** After each task, the reviewer produces a `REPORT:` block with satisfaction, risk, human review flag, and summary. Reports are stored in state and posted as bead comments.

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds

