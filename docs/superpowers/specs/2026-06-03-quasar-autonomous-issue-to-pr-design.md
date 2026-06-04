# Quasar: Autonomous GitHub Issue → PR Pipeline

**Design Spec — 2026-06-03**

## Goal

Transform Quasar from a single-nebula coder-reviewer tool into a fleet manager that ingests GitHub issues, generates and executes nebulas autonomously, and lands PRs for human review — while letting the human walk away during the work.

The stated success criterion: *"crank on tokens while only caring about the final product."*

Concretely:
- The human's only required interactions are: (1) selecting an issue to ingest, (2) approving a generated nebula in the UI, (3) reviewing the resulting PR on GitHub
- Many nebulas execute concurrently
- Crashed nebulas resume cleanly from durable state
- A safety perimeter makes destructive Git/GitHub operations impossible at runtime
- The TUI is replaced by a web UI built for fleet management

## Non-Goals (v1)

- Multi-machine coordination (single-host SQLite + filesystem only)
- Forge support beyond GitHub (GitLab/Bitbucket adapters can be added behind a `Forge` interface later)
- Auto-merging PRs (humans always merge; Quasar never can — enforced by safety perimeter)
- Multi-issue bundling into one nebula (one issue → one nebula in v1)

## Background & Constraints

- Existing Quasar (v0.1.0): Go 1.25.5, Cobra/Viper, SQLite via `internal/fabric/`, dual-agent coder-reviewer loop (`internal/loop`), nebula orchestration (`internal/nebula`), Bubble Tea TUI (`internal/tui`, ~6.5k LOC)
- The internal stack is Go-only, but Quasar *operates on* repos in any language
- Existing TUI feedback submission is broken; deep-dive per-phase views ("babysitter" ethos) are the wrong shape for fleet management
- Beads (external task-tracking CLI) is currently woven through `internal/loop` and `internal/nebula` — to be removed

---

## Section 1 — Architecture Overview

### Actor Roles

| Actor | Implementation | New? |
|---|---|---|
| Architect | Existing `internal/nebula/architect.go` | reused; new prompt adapter for issue context |
| Coder | Existing `internal/loop` coder role | reused |
| Reviewer | Existing `internal/loop` reviewer role | reused |
| Master Reviewer | New LLM role, runs once all phases close | new |
| Quasar (verb) | Targeted coder-reviewer loop spawned by master reviewer with feedback baked into the prompt | new spawn path, reuses loop |
| Human | Two checkpoints: pre-launch approval (UI), PR review (GitHub) | unchanged outside Quasar |

### Execution Topology

- **No daemon.** Each nebula runs as a detached `quasar supervise <nebula-id>` subprocess in its own process group.
- **SQLite** (extending `internal/fabric/sqlite.go`) is the runtime source of truth for cross-nebula state.
- **`state.toml`** per nebula is the durable per-nebula progress record (survives SQLite corruption, hand-editable).
- **Web UI** (`quasar serve`) is a viewer + command surface that polls SQLite, sends signals, opens `gh`. Closing the UI never kills work.
- **Reaper** runs at the start of every `quasar` invocation: stale heartbeat or dead PID → mark crashed, GC worktree.
- **Worktrees**: each nebula owns `.quasar/worktrees/<nebula-id>/` on its own `quasar/<nebula-id>` branch.
- **Logs**: each worker writes JSONL to `.quasar/logs/<worker-id>.log`.

### Lifecycle of One Nebula (happy path)

```
GitHub issue
    │
    ▼ (architect adapter parses issue → architect prompt)
[Architect] ──► nebula draft (manifest + phase .md files)
    │
    ▼ (UI presents draft; human approves/edits/refines)
[Human approval gate]
    │
    ▼ (detach supervisor)
[Supervisor] spawns phase workers ──► commits to quasar/<id> branch
    │
    ▼ (all phases close)
[Master Reviewer]
    ├──► satisfied ──► [gh pr create] ──► [Human reviews on GitHub]
    │                                          │
    │                                          ▼
    │                                    [Human triggers feedback ingest]
    │                                          │
    │                                          ▼ (gh fetches comments+CI+bots)
    └──► not satisfied ──► [Spawn fix quasar w/ feedback]
                                │
                                ▼
                            (loops back to master reviewer; cap = 3 cycles)
```

### TUI Ethos Shift

The existing TUI is built for *deep visibility into a single nebula's execution* — diffs, plans, graphs, per-phase loop views, struggle indicators. The new ethos is **fleet management**:

- All issues + all nebulas + all open PRs visible at once
- Deep-dive views are escape hatches reached only when something needs attention
- Per-phase babysitting is hidden by default; the master reviewer is responsible for "is this going off the rails" judgment

The TUI is **fully replaced** by a web UI (browser-based, see Section 7) — not patched.

### Scope Decomposition

The work is decomposed into **five sequential nebulas**, each merging as its own PR to `main`:

| Nebula | Name | Purpose | TUI work |
|---|---|---|---|
| 0 | `remove-beads` | Prerequisite cleanup; delete beads dependency + docs | none |
| 1 | `github-ingest` | GitHub client, issue→nebula adapter, safety perimeter | CLI only |
| 2 | `concurrent-orchestrator` | Supervisor, workers, worktrees, state.toml, reaper | CLI only |
| 3 | `master-review-pr-loop` | Master reviewer agent, PR creation, feedback loop | CLI only |
| 4 | `ui-rewrite` | Delete Bubble Tea TUI, build React web UI | full UI |

After each nebula merges, the system is fully runnable (CLI-only until Nebula 4 lands).

---

## Section 2 — State Model (SQLite Schema)

Extending `internal/fabric/sqlite.go`. New migrations land per-nebula. WAL mode + FK enforcement + `BEGIN IMMEDIATE` for state transitions.

### Tables

**`issues`** — local cache of GitHub issues (Nebula 1)
- `(repo TEXT, number INTEGER)` PK
- `title`, `body`, `state` (`open`/`closed`), `labels` (JSON), `assignee`, `url`
- `fetched_at` (unix; manual refresh updates)
- `dismissed` (bool; user-rejected drafts hide forever)
- Index: `(repo, state, dismissed)`

**`nebulas`** — every nebula (manual or issue-sourced) (existing, extended in Nebula 1)
- `id TEXT PK` (e.g. `nebula-42-add-auth`)
- `source_type` (`issue` / `manual`)
- `source_repo TEXT, source_issue INT` (FK → issues, nullable)
- `path TEXT` (`.nebulas/<id>/`)
- `branch TEXT` (`quasar/<id>`)
- `worktree_path TEXT` (UNIQUE)
- `status TEXT` (`draft` / `approved` / `running` / `master_review` / `pr_open` / `merged` / `crashed` / `abandoned`)
- `master_cycle INTEGER` (0..3)
- `created_at`, `updated_at`
- Index: `(status)`, `(source_repo, source_issue)`

**`phases`** — phases within a nebula (Nebula 1)
- PK: `(nebula_id, phase_id)`
- `title`, `type`, `priority`
- `depends_on` (JSON array)
- `status` (`pending` / `ready` / `running` / `review` / `complete` / `failed`)
- `started_at`, `completed_at`
- Index: `(nebula_id, status)`

**`workers`** — every spawned process (Nebula 2)
- `id TEXT PK` (UUID)
- `nebula_id` (FK)
- `kind` (`supervisor` / `phase` / `quasar` / `master_review`)
- `phase_id`, `cycle_num` (context, nullable per kind)
- `pid INTEGER, pgid INTEGER, pid_started_at INTEGER` (guards against PID reuse)
- `started_at`, `last_heartbeat`
- `status` (`running` / `exited` / `crashed` / `killed`)
- `exit_code` (nullable)
- `log_path TEXT`
- Index: `(nebula_id)`, `(status, last_heartbeat)` for reaper scan

**`master_reviews`** — one row per master review cycle (Nebula 3)
- PK: `(nebula_id, cycle_num)`
- `started_at`, `completed_at`
- `decision` (`spawn_quasar` / `open_pr` / `cap_hit_open_pr` / `crash_pr`)
- `rationale TEXT` (LLM's writeup)
- `quasar_feedback TEXT` (nullable; sent to spawned quasar)
- `verify_results TEXT` (JSON: test/lint/build outputs at time of review)

**`prs`** — opened PRs (Nebula 3)
- `nebula_id TEXT PK` (FK)
- `pr_number INTEGER, url TEXT`
- `opened_at INTEGER`
- `last_feedback_sync_at INTEGER` (nullable)
- `status` (`open` / `merged` / `closed`)

**`feedback_rounds`** — each `quasar pr respond` invocation (Nebula 3)
- `id TEXT PK` (UUID)
- `nebula_id` (FK)
- `triggered_at INTEGER`
- `source` (`auto_fetch` / `manual` / `mixed`)
- `feedback_text TEXT` (bundled prompt sent to spawned quasar)
- `worker_id TEXT` (FK → workers; the quasar that addressed it)
- Index: `(nebula_id, triggered_at DESC)`

### Design Notes

- **Issue comments are NOT cached.** Fetched on-demand at architect-prompt time.
- **Nebula `status` is denormalized.** Whoever transitions it writes the new value in the same transaction as the underlying change. Reaper reconciles inconsistency.
- **No audit history table in v1.** Master review cycles + feedback rounds give chronological forensics; add `nebula_status_history` only if debugging gets painful.

### Source-of-truth split

- **SQLite** = runtime state: live worker rows, heartbeats, fleet-wide queries. Ephemeral; regeneratable from state.toml + worktrees.
- **state.toml** = durable per-nebula progress. Survives SQLite corruption. Human-readable, hand-editable.

### Issue cache policy

- Manual refresh only (no auto-staleness UI)
- All open issues (no built-in filter; `[github].issue_filter` can constrain)
- Dismissed issues hide forever (no body-hash-based unhide)

---

## Section 3 — Issue → Nebula Flow

### Pipeline

```
gh api repos/<repo>/issues/<num>           ─► IssueDetail
gh api repos/<repo>/issues/<num>/comments  ─► [Comment]
                    │
                    ▼
              IssueContext         (struct: title, body, labels, comments, linked PRs)
                    │
                    ▼
          ArchitectPrompt          (rendered from Go template; injects issue context
                                    into the prompt the architect already understands)
                    │
                    ▼
        internal/nebula/architect   (existing; unchanged contract)
                    │
                    ▼
       .nebulas/<nebula-id>/        (manifest + phase .md files written to disk)
                    │
                    ▼
       INSERT nebulas (status='draft', source_type='issue', source_issue=<num>)
                    │
                    ▼
       UI shows in NEBULAS pane with status `draft`
```

### `IssueContext` (Go struct)

```go
type IssueContext struct {
    Repo        string      // "papapumpkin/quasar"
    Number      int
    Title       string
    Body        string      // raw markdown
    Labels      []string
    Assignee    string
    Comments    []Comment   // chronological
    LinkedPRs   []int       // PRs that reference this issue, fetched via gh search
    URL         string
}

type Comment struct {
    Author    string
    Body      string
    CreatedAt time.Time
}
```

### Architect Prompt Template

Inserted *into the existing architect prompt* (not a replacement). New file: `internal/nebula/prompt_issue.tmpl`. Contains repo name, issue number, title, labels, URL, body, comments, and linked PRs in a structured block; the existing architect instructions follow.

### Nebula ID Generation

`nebula-<issue-number>-<slug-of-title>` (e.g. issue #42 "Add OAuth login" → `nebula-42-add-oauth-login`). Slug: lowercase, ASCII, hyphenated, capped at 40 chars. Collision (re-pull after dismissal) appends `-2`, `-3`, etc.

### New CLI Commands

| command | behavior |
|---|---|
| `quasar issue refresh` | calls `gh issue list --state open --json ...` → upserts `issues` table |
| `quasar issue list` | reads from `issues` table (no fetch), prints |
| `quasar issue pull <num>` | fetches issue + comments + linked PRs, runs architect, writes `.nebulas/<id>/`, inserts row with status=`draft` |
| `quasar issue dismiss <num>` | sets `dismissed=1` |

### Human Review UX (in UI)

Six actions, all supported:
- **(a) Approve** → launches immediately
- **(b) Reject** → discards nebula, leaves issue untouched
- **(c) Edit phase files in place** → UI opens an in-browser editor (CodeMirror)
- **(d) Ask architect to refine with feedback** → user types feedback, architect re-runs
- **(e) Regenerate from scratch** → fresh architect run, replaces draft
- **(f) Edit manifest** (priority, max_review_cycles, etc.) without touching phases

### Edge Cases

- **Issue body changes after pull**: nebula draft unchanged (snapshot at pull time). User dismisses + re-pulls if needed.
- **Architect failure**: exits non-zero, no nebula row inserted, UI surfaces error.
- **Issue not found / GitHub down**: `quasar issue pull` errors out; UI shows error toast.
- **Already-pulled issue**: pulling again creates a new nebula with `-2` suffix; old nebula untouched.

### What Doesn't Change

- Architect's prompt structure (the adapter renders into the existing template's context block)
- Manual `.nebulas/<id>/` authoring (still works via `quasar nebula apply`)

---

## Section 4 — Concurrent Execution & Process Lifecycle

### Process Tree

Each nebula owns a **supervisor process** (deterministic Go, **no LLM calls, zero tokens**) that manages everything for that nebula.

```
quasar nebula apply <id>  ── exits immediately after fork ──────┐
                                                                │
fork+setsid ─► quasar supervise <id>  (detached, own pgid)      │
                       │                                        │
                       ├─ owns worktree .quasar/worktrees/<id>/ │
                       ├─ owns branch quasar/<id>               │
                       ├─ updates state.toml + SQLite           │
                       ├─ heartbeats workers row every 5s       │
                       │                                        │
                       │  while phases remain ready:            │
                       │    fork+setsid ─► quasar phase-exec    │
                       │                                        │
                       │  all phases closed:                    │
                       │    fork+setsid ─► quasar master-review │
                       │                                        │
                       │  master decides spawn_quasar:          │
                       │    fork+setsid ─► quasar phase-exec    │
                       │       (kind='quasar', cycle_num=N)     │
                       │                                        │
                       │  master decides open_pr (or cap_hit):  │
                       │    gh pr create ─► insert into prs     │
                       │    supervisor exits clean              │
└───────────────────────┴────────────────────────────────────────┘
```

**Supervisor is NOT an agent.** Token cost scales with the work (writing/reviewing code), not with elapsed time. The supervisor is a Go orchestrator. Only `phase-exec`, `master-review`, and spawned-quasar workers invoke `claude -p`.

### Isolation Guarantees

- **Worktree per nebula** — `.quasar/worktrees/<id>/`, on branch `quasar/<id>`. SQLite UNIQUE on `worktree_path` prevents double-claim.
- **Process group per worker** — `setsid` at spawn. Killing the supervisor kills the whole tree via `kill -TERM -<pgid>`.
- **No shared mutable state across nebulas** — each supervisor only touches its own worktree, branch, DB rows.

### Concurrency Knobs

| knob | location | default |
|---|---|---|
| `max_concurrent_phases_per_nebula` | `[execution].max_workers` in `nebula.toml` | 1 |
| `max_concurrent_nebulas` | `[execution].max_nebulas` in `.quasar.yaml` | 0 (unlimited) |
| `max_master_cycles` | `[execution].max_master_cycles` | 3 |

When `max_concurrent_nebulas` cap is exceeded, the supervisor writes `status='queued'` and exits without forking phase workers. The reaper-on-every-invocation (plus a periodic check in `quasar serve`) promotes queued nebulas as in-flight ones complete.

### Heartbeats & Reaper

- **Heartbeat interval:** 5s. Supervisors and phase workers touch `workers.last_heartbeat`.
- **Liveness check (reaper):**
  1. Heartbeat fresh (< 30s)? → alive
  2. Stale → `kill -0 pid` succeeds AND `pid_started_at` matches? → alive (just slow)
  3. Otherwise → dead. Mark `status='crashed'`, propagate appropriately.
- **Reaper runs:**
  - At the start of every `quasar` CLI invocation
  - Inside `quasar serve` once per 15s
  - Explicitly via `quasar reap`

### Kill Semantics

| input | result |
|---|---|
| `quasar kill <nebula-id>` | SIGTERM to supervisor's pgroup; escalates to SIGKILL after 10s |
| `quasar kill --all` | applies above to every `status='running'` supervisor; requires `--yes` if >3 |
| `quasar kill --phase <worker-id>` | SIGTERM one phase worker; supervisor decides retry/fail |
| UI kill button | HTTP POST to `/api/nebulas/:id/kill` |

### Crash Recovery via state.toml

`state.toml` (durable, per-nebula, written on every transition) is the recovery contract:

```toml
[nebula]
id = "nebula-42-add-auth"
status = "running"
master_cycle = 0
branch = "quasar/nebula-42-add-auth"
worktree = ".quasar/worktrees/nebula-42-add-auth"

[phases.scaffold]
status = "complete"
commit_sha = "abc1234"
started_at = 2026-06-03T12:00:00Z
completed_at = 2026-06-03T12:14:00Z

[phases.handlers]
status = "in_flight"
started_at = 2026-06-03T12:14:00Z

[phases.tests]
status = "pending"
depends_on = ["handlers"]

[[master_reviews]]
cycle = 1
decision = "spawn_quasar"
rationale = "Tests failing in auth_test.go::TestRefresh"
quasar_feedback = "Fix the refresh-token test..."
verify_results.test = "FAIL"
verify_results.lint = "PASS"
```

**`quasar resume <id>` behavior:**
1. Reaper marks any stale `running` workers as `crashed`
2. New supervisor reads `state.toml`
3. `complete` phases — trust, skip
4. `in_flight` phases — **re-dispatch from scratch**; phase worker prompt notes "the codebase may contain partial work from a prior interrupted run; treat it as untrusted scaffolding"
5. `pending` phases — dispatch when deps satisfied
6. Master cycle resumes from `master_cycle`

**Crash policy:**
- Phase worker crashes → supervisor retries once, then marks phase `failed` and nebula stalls awaiting human attention
- Supervisor crash → reaper marks nebula `crashed`; worktree preserved for inspection; `quasar resume <id>` re-spawns

### Logging

- Each worker writes to `.quasar/logs/<worker_id>.log` (JSONL, append-only)
- Web UI tails via SSE: `GET /api/workers/:id/log/stream`
- CLI tails via `quasar logs <nebula-id> [--follow] [--phase <id>]`

### Edge Cases

- Two `quasar nebula apply <id>` racing → SQLite UNIQUE on `worktree_path` rejects second
- System reboot → next `quasar` invocation reaps all; `quasar resume --all-crashed` resurrects en masse
- Disk full mid-clone → phase errors out, supervisor marks `failed`, surfaces error
- Phase scope conflicts → `[scope]` in phase frontmatter + `allow_scope_overlap=false` prevents

---

## Section 5 — Master Reviewer

### Trigger

Supervisor calls master reviewer when all phases have `status='complete'` AND `master_cycle < max_master_cycles`. Master review runs as its own subprocess (`quasar master-review <nebula-id> --cycle N`) with `workers.kind='master_review'`.

### Inputs (Prompt Composition)

| input | source | cache strategy |
|---|---|---|
| System prompt | static `internal/master/prompt.tmpl` | cached prefix |
| Issue context | `IssueContext` | cached until issue body changes |
| Nebula manifest + phase summaries | `state.toml` | cached for nebula lifetime |
| Full diff vs. base | `git diff <base>...HEAD` | fresh each cycle; summarized via Sonnet/Haiku if >50k tokens |
| Verify results | rerun `[verify].test/.lint/.build` | fresh each cycle |
| File-touch summary | derived from `git diff --stat` | fresh, tiny |
| Prior master reviews (this nebula) | `state.toml` `[[master_reviews]]` | fresh but small |

Claude prompt caching means cycles 2 and 3 reuse most context; cost scales with what's new.

### Output (Structured Tool Use)

Master reviewer gets exactly two tools — must call one:

```python
# Tool 1: spawn a coder-reviewer loop with targeted feedback
{
  "name": "spawn_quasar",
  "input_schema": {
    "feedback": "string (specific, actionable; include file paths)",
    "verify_signal": "enum [test_failure, lint_failure, build_failure, "
                     "code_quality, missing_implementation, other]"
  }
}

# Tool 2: ship as PR
{
  "name": "open_pr",
  "input_schema": {
    "ship_rationale": "string (why it's ready; visible in PR body)",
    "outstanding_concerns": "string (anything human reviewer should focus on; "
                            "empty string if none)"
  }
}
```

### Supervisor Reaction

```
tool=spawn_quasar:
  1. Persist [[master_reviews]] entry
  2. master_cycle += 1
  3. Fork `quasar phase-exec --kind=quasar --feedback="..." --nebula=<id>`
  4. Wait for worker completion
  5. If master_cycle < max_master_cycles: re-invoke master reviewer (cycle N+1)
  6. Else (cap hit): record decision='cap_hit_open_pr' with the just-completed
     cycle's feedback as outstanding_concerns; proceed to open PR via cap_hit.tmpl

tool=open_pr:
  1. Persist [[master_reviews]] entry
  2. Push branch quasar/<id> (via gitops.Push; validated)
  3. gh pr create (via internal/github.Client.CreatePR)
  4. Insert prs row, set nebulas.status='pr_open'
  5. Supervisor exits clean
```

### Verify Step Soft Gate

- Verify runs *before* invoking master reviewer
- Master reviewer can call `open_pr` even with failures, but must list them in `outstanding_concerns`
- PR body renders concerns prominently for human reviewer

### Three PR-Open Scenarios

| scenario | PR title prefix | body banner | `master_reviews.decision` |
|---|---|---|---|
| Master reviewer satisfied | (none) | "AI assessed this as ready to merge" | `open_pr` |
| Hit 3-cycle cap, still wanted changes | `[AI HIT REVIEW CAP]` | "AI ran 3 review cycles without converging. Outstanding concerns below." | `cap_hit_open_pr` |
| Master reviewer process kept failing | `[NEEDS HUMAN REVIEW — AUTOMATED REVIEW FAILED]` | "Automated review could not complete. Work uploaded as-is for human assessment. The AI did not vet this code." | `crash_pr` |

### Spawned Quasar's Prompt

When master reviewer says `spawn_quasar`, the spawned worker's coder prompt:

```
You are addressing feedback on prior work in this branch.

Master reviewer's feedback:
<feedback verbatim>

Verify signal category: <verify_signal>

Current state of the codebase reflects all phases of nebula <id>. Inspect the
working tree to understand what's there, then make focused changes to address
the feedback.

Do NOT redo work that's not implicated by the feedback. Stay surgical.

When you believe the feedback is addressed, commit and exit.
```

### Master Reviewer Crash Handling

If the master-review worker dies mid-call (Claude API timeout, network):
- Reaper detects crashed worker
- Supervisor re-invokes master reviewer at same `cycle_num` (no cycle increment)
- Cap on retries-per-cycle: 3 attempts
- After cap: auto-PR via `crash.tmpl` with explicit "automated review failed; human assessment required" banner

---

## Section 6 — PR Creation & Feedback Loop

### PR Creation Path

```
supervisor decides PR (open_pr | cap_hit | crash):
    │
    ▼ verify clean tree
git -C <worktree> status --porcelain  ─► must be empty
    │
    ▼ push branch (via gitops.Push, validated ref)
git -C <worktree> push origin quasar/<id> --force-with-lease
    │
    ▼ open PR (via github.Client.CreatePR)
gh pr create --base <base> --head quasar/<id> \
             --title "<rendered title>" --body "<rendered body>"
    │
    ▼ persist
INSERT INTO prs; UPDATE nebulas SET status='pr_open'; write state.toml [prs]
```

### PR Body Templates

Three templates in `internal/master/pr_templates/`:

**`ai_approved.tmpl`** — master reviewer's verdict was `open_pr`
- `Closes #<issue>`
- Summary from nebula
- Ship rationale from master reviewer
- Outstanding concerns (if any)
- Stats footer (cycles, wall time, files touched, $ spent)

**`cap_hit.tmpl`** — hit 3-cycle cap
- Banner: "AI hit the review cycle cap (3 cycles) without converging. The work is uploaded for human review."
- Last master reviewer feedback (what it still wanted)
- Verify results at upload
- Stats

**`crash.tmpl`** — master reviewer process kept failing
- Banner: "🚨 The AI's automated review process failed and could not assess this work. Treat as completely unreviewed."
- Last successful verify run (if any)
- Last partial review (if any)
- Why this happened (e.g., "Claude API errors on 3 consecutive attempts")

### Issue Closing

- `Closes #N` in PR body triggers GitHub's auto-close-on-merge
- When PR merged (detected by `quasar pr sync`): `prs.status='merged'`, `nebulas.status='merged'`, `issues.state='closed'`

### Feedback Loop: `quasar pr respond`

```
quasar pr respond <nebula-id> [--manual "<text>"]
```

**Auto path:**
1. Fetch via `gh`: PR review comments, PR conversation comments, CI check results, timeline (bot comments)
2. Filter to items new since `prs.last_feedback_sync_at`
3. Bundle into structured FeedbackBundle markdown
4. Persist into `feedback_rounds`
5. Spawn fix quasar (`quasar phase-exec --kind=quasar --feedback="..." --nebula=<id>`)
6. On success: supervisor pushes commits to `quasar/<id>`; PR auto-updates; `last_feedback_sync_at = NOW()`

**Manual path** (`--manual "<text>"`):
Same as auto except `feedback_text` is the user's text verbatim; skips `gh` fetch; `source='manual'`.

**Mixed path** (`--manual` + auto):
User text appended to auto-fetched bundle.

### Spawned Fix-Quasar Prompt

```
You are addressing feedback on PR #<num> (nebula <id>).

The PR was opened with the work below. Reviewers have left feedback.
Your job: address the feedback. Stay surgical. Do not rewrite unrelated code.

## Feedback (bundled)
<feedback_text>

## Existing PR Context
- Branch: quasar/<nebula-id>
- Base: <base>
- Closes issue: #<num> "<title>"
- Files in the diff so far: <file list>

Work on the existing branch. When you believe the feedback is addressed,
commit and exit. Do NOT amend prior commits — add new ones.
```

### After Fix Quasar Runs

- New commits pushed to `quasar/<id>` → PR auto-updates
- `prs.last_feedback_sync_at = NOW()`
- Master reviewer does **NOT** auto-re-run after a `pr respond` — assumption is the human is in the loop on GH

### PR State Sync

`quasar pr sync` polled (manually or by `quasar serve` background loop) to detect:
- PR merged → mark nebula `merged`, mark issue closed
- PR closed without merge → mark nebula `abandoned`
- New comments since last sync → flag in UI

Sync happens:
- On every `quasar` CLI invocation (lightly, only `status='open'` PRs)
- Every 30s in `quasar serve`
- Explicitly via `quasar pr sync [<nebula-id> | --all]`

### `gh` Authentication

- User runs `gh auth login` once; Quasar never touches GitHub credentials directly
- `quasar doctor` checks `gh auth status` and prints `gh auth login` reminder if missing
- `[github].repo` in `.quasar.yaml` overrides auto-detected `origin` remote

### Safety Perimeter

**Allowlist (the only writes Quasar may perform):**

| operation | allowed? | notes |
|---|---|---|
| `git push origin quasar/<nebula-id>` | ✅ | ref name MUST start with `quasar/` |
| `gh pr create` | ✅ | head MUST be a `quasar/*` branch |
| All read operations | ✅ | unrestricted |

**Denylist (Quasar must NOT do, ever):**

- `git push origin <base-branch>` or any non-`quasar/*` ref
- `git push --force` to any non-`quasar/*` ref
- `git push origin --delete <any-non-quasar-branch>`
- `git branch -D <base-branch>`, reset/rebase against base
- `gh pr merge` / `gh pr close`
- `gh issue close` / `gh issue delete` / `gh issue edit`
- `gh repo delete` / `gh repo edit`
- `gh label create/delete/edit`
- `gh release create/delete`
- `gh api` with DELETE/PUT/PATCH/POST except for PR creation

### Enforcement Layers (Defense in Depth)

**Layer 1 — Wrapper packages:**
- `internal/github/` exposes a `Client` with whitelisted methods only
  - Read: `ListIssues`, `GetIssue`, `GetIssueComments`, `GetPR`, `GetPRComments`, `GetPRChecks`, `GetPRTimeline`, `SearchPRsLinkedToIssue`, `AuthStatus`
  - Write: `CreatePR` (the ONLY write method)
  - **No** `MergePR`, `ClosePR`, `CloseIssue`, `AddLabel`, etc. exist as methods
- `internal/gitops/` exposes validated git operations
  - `Push(branch)` errors if `!strings.HasPrefix(branch, "quasar/")`
  - `DeleteRemoteBranch(branch)` same check
  - No `PushForce` on non-quasar branches; no `ResetBranch` on base branches

**Layer 2 — Architecture tests:**
- `internal/arch_test/safety_test.go`:
  - `TestNoDirectGhExec` — no `exec.Command("gh", ...)` outside `internal/github/`
  - `TestNoDirectGitPush` — no `git push` outside `internal/gitops/`
  - `TestNoForbiddenGhSubcommands` — grep entire repo for forbidden subcommands as string literals
  - `TestNoForbiddenGitOps` — similar for git

**Layer 3 — Supervisor owns all pushes:**
- Phase workers and spawned quasars commit only locally
- Supervisor (Go, no LLM) is the only thing calling `gitops.Push`

**Layer 4 — Agent prompt + tool constraints:**
- Standardized safety boundary block in every agent system prompt explicitly lists forbidden operations
- `claude -p` invocations include PreToolUse hook (via `.claude/settings.json` in worker contexts) that intercepts Bash commands matching forbidden patterns (`git push origin main`, `gh pr merge`, etc.) and blocks them

### Documentation

`docs/safety.md` lands in Nebula 1, explaining the perimeter for human maintainers.

### Edge Cases

- **Force-with-lease fails on push** (someone manually pushed to `quasar/<id>`): mark nebula `crashed`, surface in UI
- **`gh pr create` fails**: supervisor doesn't transition to `pr_open`; can retry via `quasar pr open <id>`
- **Feedback round runs while previous is in flight**: SQLite UNIQUE prevents two concurrent fix quasars on same nebula
- **Issue body deleted on GitHub between phases and PR open**: title falls back to manifest; body uses cached issue body; `Closes #N` directive still works

---

## Section 7 — CLI + Web UI Surface

### Complete CLI Surface

| command | nebula | purpose |
|---|---|---|
| **Setup** | | |
| `quasar init` | 1 | scaffold `.quasar.yaml`, auto-detect language for `[verify]` |
| `quasar doctor` | 1 | check `gh auth`, git config, `.quasar/` permissions, SQLite schema |
| `quasar version` | existing | print version (stdout) |
| **Issues** | | |
| `quasar issue refresh` | 1 | fetch all open issues from GH |
| `quasar issue list` | 1 | print cached issues |
| `quasar issue pull <num>` | 1 | run architect on issue → create draft nebula |
| `quasar issue dismiss <num>` | 1 | hide forever |
| **Nebulas** | | |
| `quasar nebula validate <path>` | existing | unchanged |
| `quasar nebula apply <path>` | 2 (extended) | fork detached supervisor; exit |
| `quasar nebula reject <id>` | 1 | delete a `draft` nebula |
| `quasar nebula refine <id> --feedback "..."` | 1 | re-run architect with feedback |
| `quasar nebula edit <id>` | 1 | open `$EDITOR` on phase files |
| **Fleet** | | |
| `quasar ps [--all]` | 2 | list active (or all) workers |
| `quasar logs <id> [--follow] [--phase <p>]` | 2 | tail worker log |
| `quasar kill <nebula-id>` | 2 | SIGTERM supervisor's pgroup |
| `quasar kill --phase <worker-id>` | 2 | kill one phase worker |
| `quasar kill --all [--yes]` | 2 | kill every running supervisor |
| `quasar reap` | 2 | explicit reaper run |
| `quasar resume <nebula-id>` | 2 | resume crashed nebula via state.toml |
| `quasar resume --all-crashed` | 2 | resume every crashed nebula |
| `quasar abandon <nebula-id>` | 2 | discard nebula, remove worktree, delete local branch |
| **PRs** | | |
| `quasar pr respond <id> [--manual "..."]` | 3 | ingest PR feedback, spawn fix quasar |
| `quasar pr sync [<id> \| --all]` | 3 | poll GH for PR state changes |
| `quasar pr open <id>` | 3 | manual retry if initial `gh pr create` failed |
| **Internal** (called by supervisor; user-runnable for debug) | | |
| `quasar supervise <id>` | 2 | the detached supervisor entrypoint |
| `quasar phase-exec --nebula <id> --phase <p>` | 2 | one phase worker |
| `quasar phase-exec --kind quasar --nebula <id> --feedback "..."` | 3 | spawned fix quasar |
| `quasar master-review <id> --cycle N` | 3 | one master review pass |
| **UI** | | |
| `quasar serve [--bind addr] [--dev]` | 4 | launch web UI |

### Web UI Architecture (Nebula 4)

**Routing:**

```
/                       Dashboard (3 cards: issues, nebulas, PRs at a glance)
/issues                 Issue list (search, "refresh" button)
/issues/:repo/:num      Issue detail ("Pull as nebula" CTA)
/nebulas                Nebula list (status filters)
/nebulas/:id            Nebula detail (phases, state, diff preview, logs)
/nebulas/:id/review     Pre-launch review (a-f from Section 3)
/prs                    PR list (status, last sync, new-comment badges)
/prs/:id                PR detail (linked nebula, comments, CI checks, Respond)
/workers/:id            Worker detail (live log stream via SSE)
/settings               .quasar.yaml editor
```

**JSON API** (under `/api/`): one endpoint per CLI action plus `GET /api/events` (global SSE for live updates: status changes, new comments, worker exits, master review decisions).

**Auth model:**
- Default `quasar serve` binds `127.0.0.1:8080` — no auth
- `quasar serve --bind 0.0.0.0:8080` generates a token (16 bytes, base64), writes to `.quasar/serve-token` (0600), prints URL with `?token=` once
- Subsequent requests require token via header / query / cookie
- Token rotates on next non-localhost bind

**Frontend stack:**

- Vite + React 18 + TypeScript (strict)
- Tailwind CSS
- TanStack Query (data fetching + cache + SSE integration)
- react-router-dom
- react-diff-viewer-continued (diff display)
- react-markdown + remark-gfm (issue/PR body rendering)
- `@codemirror/*` (in-browser phase + manifest editor)
- date-fns

**Live updates:**
- Single global SSE connection (`GET /api/events`) for status changes
- Per-worker SSE for log tailing (on-demand)
- React Query invalidates relevant caches on event receipt
- Exponential backoff reconnect

**Frontend file structure:**

```
internal/server/web/
    src/
        api/                 fetch helpers, SSE client
        components/          shared UI (Card, StatusBadge, DiffViewer, ...)
        routes/              one folder per top-level route
        hooks/
        types/               TS types mirroring Go API DTOs
        App.tsx
        main.tsx
    package.json
    vite.config.ts
    tailwind.config.ts
    tsconfig.json
    dist/                    .gitignored; built by `vite build`; embedded
```

**Embedding:**

```go
// internal/server/embed.go
package server

import "embed"

//go:embed dist
var distFS embed.FS

// served from / via net/http.FileServer wrapping fs.Sub(distFS, "dist")
// SPA fallback: 404s in dist → serve index.html
```

**Build:**

```makefile
build-ui:
	cd internal/server/web && npm ci && npm run build
build: build-ui
	go build -o quasar .
```

CI runs `make build` so embedded `dist/` is always fresh. Devs without Node get a friendly error from `make build` with `make build-go-only` for a binary requiring `quasar serve --dev` (which reverse-proxies to Vite dev server).

**Dev mode:**
- `quasar serve --dev` reverse-proxies `/` and assets to `http://localhost:5173`
- JSON API still served from quasar process
- HMR + fast iteration

### Bubble Tea Removal

In Nebula 4's final commit:
- All of `internal/tui/` deleted (~6,500 LOC)
- `cmd/tui.go` deleted; `quasar tui` removed
- Bubble Tea / lipgloss / bubbles dropped from `go.mod`

### CLI-First Principle

Every UI action has a CLI equivalent. The UI is a JSON-API client, not a parallel implementation. Consequences:
- Nebula 4 can ship later without blocking anything
- CI tests cover both surfaces (share core code)
- Terminal-preferring users never have to launch a browser

---

## Section 8 — Config, Migration & Sequencing

### `.quasar.yaml` Extensions

```yaml
[github]                       # Nebula 1
repo: ""                       # empty = auto-detect from `git remote get-url origin`
base_branch: "main"
issue_filter:
  state: "open"
  labels: []
  assignee: ""

[verify]                       # Nebula 3
test: ""                       # empty = skip; populated by `quasar init` auto-detect
lint: ""
build: ""
timeout: "5m"

[execution]                    # existing; extended
max_concurrent_nebulas: 0      # 0 = unlimited (Nebula 2)
max_master_cycles: 3           # Nebula 3
phase_retry_count: 1           # Nebula 2

[serve]                        # Nebula 4
default_bind: "127.0.0.1:8080"
sse_heartbeat: "30s"
log_retention: "30d"

[safety]                       # Nebula 1
allowed_base_branches: ["main", "master", "develop"]
forbidden_push_patterns: ["main", "master", "develop"]
```

### `quasar init` Auto-Detection (Nebula 1)

Detects language by marker file in CWD:

| marker | test | lint | build |
|---|---|---|---|
| `go.mod` | `go test ./...` | `go vet ./...` | `go build ./...` |
| `package.json` | `npm test` | `npm run lint` | `npm run build` |
| `Cargo.toml` | `cargo test` | `cargo clippy -- -D warnings` | `cargo build` |
| `pyproject.toml` | `pytest` | `ruff check .` | — |
| `Gemfile` | `bundle exec rake test` | `bundle exec rubocop` | — |
| `mix.exs` | `mix test` | `mix credo` | `mix compile` |
| `pom.xml` | `mvn test` | — | `mvn compile` |
| `build.gradle` | `gradle test` | — | `gradle build` |
| `Makefile` with `test:` | `make test` | `make lint` if present | `make build` if present |
| (none) | `""` | `""` | `""` |

Idempotent: preserves existing values, only adds missing keys.

### SQLite Migration Sequence

Forward-only, tracked in existing `_schema_migrations` (or equivalent — to be verified during Nebula 0 authoring).

| nebula | migration | scope |
|---|---|---|
| 0 | `NNN_drop_beads.sql` | drop bead-FK columns or tables (likely minimal; beads was external CLI) |
| 1 | `NNN_github_ingest.sql` | create `issues`; extend `nebulas` (`source_type`, `source_repo`, `source_issue`, `path`, `branch`, `worktree_path`, `status`); create `phases` if absent |
| 2 | `NNN_orchestrator.sql` | create `workers`, `master_reviews` (schema), `feedback_rounds` (schema) + indexes |
| 3 | `NNN_pr_loop.sql` | create `prs` + indexes; `master_reviews`/`feedback_rounds` populated meaningfully |
| 4 | none | UI is read-only on schema |

### Filesystem Layout (Final, post-Nebula 4)

```
<project root>/
├── .quasar.yaml                          extended config
├── .quasar/
│   ├── quasar.db, *.db-shm, *.db-wal    SQLite
│   ├── serve-token                       0600; only when --bind non-localhost
│   ├── worktrees/<nebula-id>/            Nebula 2 — git worktrees
│   └── logs/<worker-id>.log              Nebula 2 — JSONL per worker
├── .nebulas/
│   └── <nebula-id>/
│       ├── nebula.toml
│       ├── <phase>.md
│       └── state.toml                    Nebula 2 — durable progress
├── .git/
│   └── worktrees/<nebula-id>             git's metadata
└── docs/
    └── safety.md                         Nebula 1 — perimeter doc
```

### Backward Compatibility

- Pre-existing `.nebulas/*` without `state.toml`: first supervisor run generates one from phase files
- Pre-existing `.quasar.yaml` without new sections: defaults apply; missing `[github]` disables GitHub features; missing `[verify]` skips verify gates
- Pre-existing `quasar nebula apply <path>` invocations: work; go through new supervisor path internally
- Older SQLite schema: migrations apply forward on first post-upgrade invocation

### Removal Targets per Nebula

| nebula | net deletion |
|---|---|
| 0 | ~1.6k LOC pure (`internal/beads/`, `tui/beadview.go`, `loop/bead_hook.go`) + ~500 LOC scattered references |
| 1 | 0 — purely additive |
| 2 | 0 net; heavy refactor of `tui/model.go` (2,398 LOC) into router + per-pane models |
| 3 | 0 — purely additive |
| 4 | ~6,500 LOC: all of `internal/tui/`, `cmd/tui.go`, Bubble Tea deps |
| **net** | **~8,600 LOC removed**, ~10,000 LOC added |

### Dependency Order

```
Nebula 0 (remove-beads)
    ▼
Nebula 1 (github-ingest)
    ▼
Nebula 2 (concurrent-orchestrator)
    ▼
Nebula 3 (master-review-pr-loop)
    ▼
Nebula 4 (ui-rewrite)
```

Each nebula merges as its own PR. After each merge, the system is fully runnable (CLI-only until 4 lands). Pause-able at any nebula boundary without leaving the system broken.

### Bootstrap Loop

This entire revamp can be executed by Quasar's existing (pre-revamp) self running each of nebulas 0-4 as hand-authored manifests. Each nebula makes Quasar slightly more capable of executing the next nebula. By Nebula 4, the autonomous version of Quasar is shipping its own UI. This is both the right validation strategy and the right test of the design.

### Cost Sketch (rough)

| nebula | phases | est. cost |
|---|---|---|
| 0 | 3-4 | $5-15 |
| 1 | 5-7 | $20-50 |
| 2 | 6-8 | $40-80 |
| 3 | 5-7 | $30-60 |
| 4 | 6-9 | $40-80 |
| **total** | **25-35** | **$135-285** |

SWAGs. Recalibrate after Nebula 0 ships.

---

## Open Items / Risks

- **Phase scope conflicts under concurrency** (Nebula 2): existing `[scope]` + `allow_scope_overlap` mechanisms in nebula format should handle this; worth validating with a phase that intentionally races. Falls out of Nebula 2 phase design.
- **Claude API rate limits at high nebula concurrency**: a fleet of 10 concurrent nebulas × 4 phases each = 40 simultaneous `claude -p` invocations. May hit per-account rate limits. Mitigation: backoff + retry in phase worker; soft cap on `max_concurrent_nebulas` if encountered.
- **State.toml ↔ SQLite drift**: in theory a supervisor crash between writing state.toml and committing the SQLite txn could desync. Mitigation: reaper detects inconsistency (worker dead but state.toml says `in_flight` for that phase) and prefers state.toml as the more durable record.
- **`gh` CLI not installed**: `quasar doctor` catches this and prints install instructions; without `gh`, GitHub features are unavailable but manual `.nebulas/*` still works.
- **Large diff summarization fallback** (Section 5): adds a small extra LLM call per master review cycle when diff > 50k tokens. Cost is bounded but non-zero. Alternative would be hard-truncation (lose context). Currently going with summarization for quality.

## Success Criteria

- Pull an issue from GitHub via `quasar issue pull <num>` → see a draft nebula in the UI
- Approve the draft → supervisor spawns; phases run; commits land on `quasar/<id>` branch
- Master reviewer runs on completion; either opens PR or spawns fix quasar
- PR opens on GitHub via `gh`; closes the original issue on merge
- `quasar pr respond <id>` ingests new feedback, spawns a fix quasar, pushes new commits
- Many nebulas in flight concurrently; UI shows fleet at a glance
- Closing the UI does not kill work; reopening shows current state
- Crashed nebulas resume via `quasar resume`
- Quasar cannot push to `main` or merge a PR by construction (test passes; runtime check rejects)
