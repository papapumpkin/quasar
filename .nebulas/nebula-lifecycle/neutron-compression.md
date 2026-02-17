+++
id = "neutron-compression"
title = "Neutron compression: condense completed nebula into portable archive"
type = "feature"
priority = 1
depends_on = ["constellation-engine"]
+++

## Problem

A completed nebula generates a lot of artifacts: phase `.md` files, state file, agent outputs, reviewer reports, bead hierarchies, git diffs, cost/timing data. Once the work is done and accepted, keeping all of this in raw form is wasteful — but throwing it away means losing valuable context for future work. The neutron phase compresses everything into a dense, portable artifact that preserves all information but takes minimal space.

## Concept

A neutron star is the collapsed core of a star — incredibly dense, preserving all the mass in a tiny volume. Similarly, a neutron archive is the collapsed form of a nebula — all the context, decisions, outputs, and metadata packed into a single structured file that can be:

- **Referenced**: "What did we decide about auth middleware?" → search the neutron
- **Continued**: "We need to extend this work" → expand the neutron back to a full nebula
- **Tracked**: Feed neutron summaries into project-level dashboards
- **Chained**: Use as input context for a new nursery nebula ("based on the auth work, now add rate limiting")

## Solution

### 1. Neutron Archive Format

A neutron is a single `.neutron.json` (or `.neutron.gz` for compressed) file:

```json
{
  "version": 1,
  "nebula": {
    "name": "auth-feature",
    "description": "Add authentication to the API",
    "created_at": "2026-02-15T10:00:00Z",
    "completed_at": "2026-02-16T14:30:00Z",
    "lifecycle_stage": "neutron"
  },
  "summary": {
    "total_phases": 6,
    "phases_done": 5,
    "phases_failed": 1,
    "total_cost_usd": 2.84,
    "total_cycles": 14,
    "total_duration_seconds": 500,
    "files_changed": 12,
    "lines_added": 340,
    "lines_removed": 89
  },
  "phases": [
    {
      "id": "setup-models",
      "title": "Set up data models",
      "status": "done",
      "satisfaction": "high",
      "risk": "low",
      "cycles": 2,
      "cost_usd": 0.15,
      "duration_seconds": 12,
      "bead_id": "quasar-abc",
      "description_hash": "sha256:...",
      "description": "Set up the User and Session models...",
      "coder_summary": "Created models/user.go and models/session.go...",
      "reviewer_summary": "Clean implementation, proper error handling.",
      "diff_stat": "user.go | 45 ++++\nsession.go | 32 ++++",
      "children_beads": ["quasar-abc.1", "quasar-abc.2"],
      "depends_on": [],
      "context_goals": ["Add authentication to all API endpoints"]
    }
  ],
  "context": {
    "repo": "github.com/example/myproject",
    "goals": ["..."],
    "constraints": ["..."]
  },
  "execution": {
    "max_workers": 2,
    "model": "claude-opus-4-6",
    "config_snapshot": {}
  }
}
```

### 2. Compression Strategy

The key insight: **summarize, don't truncate**. Instead of just cutting output, use an AI agent to condense:

- **Full coder output** (potentially thousands of lines) → **coder summary** (1-3 sentences capturing what was done)
- **Full reviewer output** → **reviewer summary** (the REPORT section, already structured)
- **Full git diff** → **diff stat** (file-level summary: `file.go | 45 ++++`)
- **Phase descriptions** → kept in full (they're already concise)
- **Bead references** → IDs only (can be looked up via `bd show`)

For the AI summarization, use the existing invoker with a "summarizer" agent that reads coder/reviewer output and produces concise summaries. This runs once per phase during compression.

### 3. Decompression (Expand)

`nebula expand <neutron-file>` reconstructs a full nebula directory:

1. Create the directory structure with `nebula.toml` from the archive's `nebula` and `context` sections
2. Recreate each phase `.md` file from `description` (the full text is preserved)
3. Create a `nebula.state.toml` reflecting the archived state
4. Mark the lifecycle stage as `constellation` (so the user can review and iterate)
5. Summaries, costs, and reviewer reports go into the state file

The expanded nebula is ready for:
- **Review**: Open in constellation mode to see what was done
- **Continuation**: Add new phases that build on the completed work
- **Re-run**: Reset specific phases to re-execute with updated context

### 4. CLI Commands

```bash
# Compress a completed constellation to neutron
quasar nebula neutron .nebulas/auth-feature/
# → Creates .nebulas/auth-feature/auth-feature.neutron.json

# Expand a neutron back to a full nebula
quasar nebula expand auth-feature.neutron.json --dir .nebulas/auth-feature-v2/

# Show neutron summary without expanding
quasar nebula neutron show auth-feature.neutron.json
```

### 5. Summarizer Agent

A lightweight agent that reads raw output and produces concise summaries:

```go
func SummarizePhaseOutput(ctx context.Context, invoker agent.Invoker, coderOutput, reviewerOutput string) (coderSummary, reviewerSummary string, err error)
```

System prompt: "You are a technical summarizer. Given the raw output of a coding agent, produce a 1-3 sentence summary of what was accomplished. Focus on: what files were changed, what was the key decision or approach, and the outcome."

Budget: very low per invocation ($0.01-0.05 per phase), uses the cheapest available model.

### 6. Neutron as Context for New Nebulas

A nursery nebula's `[context]` section could reference neutron archives:

```toml
[context]
prior_work = ["auth-feature.neutron.json"]
```

The phase summaries from the neutron would be injected into coder/reviewer prompts as background context: "Previously, the team completed: [summaries]."

## Files to Create

- `internal/nebula/neutron.go` — `NeutronArchive` type, `Compress()`, `Expand()`, `Show()`
- `internal/nebula/neutron_test.go` — Tests with mock data
- `internal/nebula/summarizer.go` — AI summarizer agent for output condensation
- `cmd/neutron.go` — `nebula neutron`, `nebula expand` commands

## Files to Modify

- `internal/nebula/types.go` — Add `NeutronArchive` struct if not in neutron.go
- `internal/nebula/state.go` — Read phase outputs/diffs for compression input
- `cmd/nebula.go` — Register neutron/expand subcommands

## Acceptance Criteria

- [ ] `nebula neutron <path>` compresses a completed nebula to a `.neutron.json` file
- [ ] Archive contains: all phase descriptions, summaries, costs, timings, diff stats, bead refs
- [ ] AI summarizer condenses coder/reviewer output to 1-3 sentences per phase
- [ ] `nebula expand <file>` reconstructs a full nebula directory from the archive
- [ ] Expanded nebula opens in constellation mode for review
- [ ] `nebula neutron show <file>` displays archive summary without expanding
- [ ] Compression is lossless for structured data (descriptions, costs, beads)
- [ ] Summaries capture the essential what/why of each phase
- [ ] `go build` and `go test ./...` pass
