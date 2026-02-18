+++
id = "prior-art-survey"
title = "Survey existing multi-agent codebase coordination solutions"
type = "task"
priority = 1
depends_on = []
+++

## Problem

Multiple AI agents working on the same codebase is a growing pattern but coordination is hard. We need to understand what solutions already exist before building our own.

## Research Areas

1. **Existing tools and frameworks**:
   - How does Devin handle multi-file work? Does it use file locking?
   - How does Cursor's multi-file agent coordinate changes?
   - How does OpenHands (formerly OpenDevin) handle concurrent tool use?
   - How does SWE-agent handle file conflicts?
   - How does Aider handle multiple files and git integration?

2. **Coordination patterns from distributed systems**:
   - Optimistic concurrency control (git merge model)
   - Pessimistic locking (file-level locks, flock)
   - CRDT-based approaches for concurrent edits
   - Lock-free designs using message passing

3. **Git-native approaches**:
   - Worktrees: each agent gets its own worktree, merge at boundaries
   - Branch-per-agent with automated merge
   - Stacked diffs / patch queues
   - Git's own lock file mechanism (.lock files)

4. **Scope-based partitioning**:
   - Quasar already has phase `scope` fields (glob patterns for owned files)
   - Could scope be the primary conflict-avoidance mechanism?

## Deliverable

A design document (written as notes on this bead or as a memory file) summarizing:
- What existing tools do and their limitations
- Recommended approach for quasar (with tradeoffs)
- Whether scope-based partitioning is sufficient or if we need runtime locking

## Acceptance Criteria

- [ ] Survey covers at least 3 existing tools/approaches
- [ ] Identifies pros/cons of each approach for quasar's use case
- [ ] Recommends a concrete direction with justification
