+++
id = "constellation-runtime"
title = "Constellation runtime: DAG walker, state machine, builtin operators, per-(repo,nebula) execution, sensor backpressure"
type = "task"
priority = 2
depends_on = ["file-loader-and-discrimination", "nebula-to-sqlite-migration", "github-sensor-produces-nebula"]
scope = [
    "internal/constellations/runtime.go",
    "internal/constellations/runtime_test.go",
    "internal/constellations/state.go",
    "internal/constellations/builtins.go",
    "internal/constellations/builtins_test.go",
    "internal/constellations/operators/**",
    "internal/fabric/constellation_store.go",
    "internal/fabric/constellation_store_test.go",
    "internal/fabric/migrations/**",
    "internal/supervisor/supervisor.go",
    "internal/supervisor/supervisor_test.go",
    "cmd/supervise.go",
]
+++

## Problem

This is the load-bearing phase: the runtime that actually executes constellations. Everything else is infrastructure for this. The runtime:
- Loads a constellation from the file loader (Phase 2)
- Reads its target nebula from the SQLite store (Phase 3)
- Walks the DAG: dispatches each node based on its `type` (star, constellation, phase_iterator, builtin)
- Evaluates `when:` expressions against the running state to decide which edges to follow
- Persists state at every transition for crash-safe resume
- Threads `[pre_commit]` from the repo's config into every gitops.Commit
- Enforces sensor backpressure (no more than `max_inflight` runs per repo per sensor)

Plus the supervisor: a long-running process that boots all the schedulers, listens for sensor triggers, instantiates constellation runs, and survives crashes via SQLite-backed state recovery.

## Solution

### SQLite additions

New migration `internal/fabric/migrations/NNN_constellation_runs.sql`:

```sql
CREATE TABLE constellation_runs (
  id                       INTEGER PRIMARY KEY AUTOINCREMENT,
  repo_path                TEXT NOT NULL REFERENCES repos(path) ON DELETE CASCADE,
  constellation_name       TEXT NOT NULL,
  constellation_snapshot   BLOB NOT NULL,         -- snapshotted TOML at instantiation
  nebula_id                TEXT NOT NULL REFERENCES nebulas(id) ON DELETE CASCADE,
  parent_run_id            INTEGER REFERENCES constellation_runs(id) ON DELETE SET NULL,

  status                   TEXT NOT NULL DEFAULT 'pending',
  started_at               INTEGER NOT NULL,
  completed_at             INTEGER,
  dag_state_toml           TEXT,                  -- DAG walker state for resume
  current_node_id          TEXT,
  cycle                    INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX constellation_runs_status     ON constellation_runs (status);
CREATE INDEX constellation_runs_repo_neb   ON constellation_runs (repo_path, nebula_id);

CREATE TABLE star_invocations (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id          INTEGER NOT NULL REFERENCES constellation_runs(id) ON DELETE CASCADE,
  node_id         TEXT NOT NULL,
  star_name       TEXT NOT NULL,
  cycle           INTEGER,
  cost_usd        REAL,
  duration_ms     INTEGER,
  started_at      INTEGER NOT NULL,
  completed_at    INTEGER,
  rationale_blob_hash  TEXT,                -- the LLM's full reasoning text
  rationale_preview    TEXT,                -- first 500 chars (for TUI)
  parsed_result_toml   TEXT
);

CREATE INDEX star_invocations_run ON star_invocations (run_id);

CREATE TABLE trigger_queue (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  repo_path        TEXT NOT NULL REFERENCES repos(path) ON DELETE CASCADE,
  constellation    TEXT NOT NULL,
  nebula_id        TEXT NOT NULL REFERENCES nebulas(id) ON DELETE CASCADE,
  enqueued_at      INTEGER NOT NULL,
  dispatched_at    INTEGER
);

CREATE INDEX trigger_queue_pending ON trigger_queue (dispatched_at) WHERE dispatched_at IS NULL;
```

### Runtime

`internal/constellations/runtime.go`:

```go
type Runtime struct {
    db          *sql.DB
    blobs       *blobstore.Store
    nebulaStore *fabric.NebulaStore
    runStore    *fabric.ConstellationRunStore
    loader      *artifacts.Loader  // per-repo file loader
    gitops      *gitops.Client     // per-repo gitops client
    invoker     agent.Invoker      // LLM invocation
    repoPath    string
    repoCfg     config.Config      // includes [pre_commit]
}

func New(opts RuntimeOpts) *Runtime

// Fire instantiates a constellation run against a nebula. Snapshots the
// constellation TOML at this moment for versioning. Returns the run ID.
// The actual execution happens asynchronously — caller polls Status or
// waits for the run to reach a terminal state.
func (r *Runtime) Fire(ctx context.Context, constellationName, nebulaID string, parentRunID *int64) (runID int64, err error)

// Run advances a single constellation run by one node, persisting state
// after each transition. Returns the next status. Called repeatedly by the
// supervisor's dispatch loop until the run reaches a terminal status.
func (r *Runtime) Step(ctx context.Context, runID int64) (newStatus string, err error)

// Resume picks up a constellation run that was interrupted. Reads
// dag_state_toml, restores the State, continues from current_node_id.
func (r *Runtime) Resume(ctx context.Context, runID int64) error
```

### State machine

`internal/constellations/state.go`:

```go
// State is the runtime evaluation context for a constellation run. It is
// what `when:` expressions evaluate against. State accumulates as nodes
// complete.
type State struct {
    Inputs map[string]any     // constellation inputs (not used in our model — nebula is the universal input)
    Nodes  map[string]any     // populated as each node completes; e.g. State.Nodes["review"]["approved"]
    Nebula NebulaSnapshot     // a denormalized snapshot of the nebula row (name, source, context, phases)
    Cycle  int                // master review cycle, or DAG iteration count
    Meta   MetaSnapshot       // total_cost_usd, run_started_at, etc.
}

// MarshalState produces the dag_state_toml column value for resume.
func MarshalState(s *State) (string, error)
func UnmarshalState(toml string) (*State, error)
```

### Node dispatch

The runtime's `Step` method dispatches based on `Node.Type`:

- **`star`**: invoke the LLM via the `agent.Invoker`. Pre-commit is threaded through gitops automatically when the star commits. The star's prompt is built from its Markdown body plus skill fragments (resolved at load time by Phase 2). Outputs (committed SHA, findings, etc.) are stored in `state.Nodes[node.id]`.
- **`constellation`**: recursively `Fire` a sub-constellation run with `parent_run_id` set. The parent's Step blocks (semantically — actually it returns a pending status and the supervisor schedules the child) until the child reaches a terminal status.
- **`phase_iterator`**: for each phase in `nebula.Phases`, fire a sub-constellation run with `parent_run_id` set. The parent waits for all children to reach terminal status. Parallelism cap from `inputs.parallel`.
- **`builtin`**: dispatch to a named operator. See operators below.

After a node completes, edges are evaluated: for each `[[edge]]` whose `from` matches the just-completed node, evaluate `when` against state. The first matching edge's `to` becomes the next node. If `to` is `_done`, `_failed`, `_awaiting_human`, or `_paused`, the run reaches that terminal status.

### Builtin operators

`internal/constellations/operators/`:

- `render_seed_prompt` — renders a seed nebula into the architect's prompt (replaces ticket-ingest's prompt_ticket.go pattern). Reads nebula columns; produces a Markdown string.
- `persist_phases` — parses the architect's structured output (an array of phase specs in TOML) and inserts them into the `phases` table via the nebula store
- `gh_open_pr` — pushes the branch + opens a PR via the existing gitops.Client (uses Forge interface stub from ticket-ingest; full PR-comment-polling and PR-status-sync come in a future nebula)
- `verify_test`, `verify_lint`, `verify_build` — run the configured `[verify].test/.lint/.build` commands and produce structured outcomes
- `notify_human` — sets the nebula status to `awaiting_human` and surfaces in the TUI

Each operator is a Go function with the signature:

```go
type Operator func(ctx context.Context, runtime *Runtime, state *State, args map[string]any) (output map[string]any, err error)
```

Operators register themselves in a package-level map via `init()`.

### Pre-commit integration

The runtime instantiates a `gitops.Client` for the repo on `Fire`. Every time a star invocation needs to commit, it calls `runtime.gitops.Commit(ctx, message, opts)` where `opts.PreCommit` was populated at runtime construction time from `runtime.repoCfg.PreCommit`. The star never sees pre-commit config. The runtime never makes per-call decisions about it. It just always applies.

If pre-commit fails and `FailOnError` is true, the commit attempt errors. The star sees the error in its loop and decides what to do (typically: address the failure and retry).

### Supervisor

`internal/supervisor/supervisor.go`:

```go
type Supervisor struct {
    db        *sql.DB
    registry  *repos.Registry
    runtimes  map[string]*constellations.Runtime  // keyed by repo_path
    schedulers []*sensors.Scheduler
}

func New(opts SupervisorOpts) *Supervisor

// Run boots all per-repo runtimes and sensor schedulers, then enters the
// dispatch loop. Returns only when ctx is canceled or a fatal error
// occurs. Survives sensor crashes (logs and restarts the affected
// scheduler) but not runtime crashes (those propagate; OS-level
// supervisor like systemd handles restart).
func (s *Supervisor) Run(ctx context.Context) error
```

Boot sequence:
1. Run migrations (idempotent)
2. Reaper: scan `constellation_runs` for `status='running'` rows older than 60s. Mark them `crashed`. Surface in the TUI.
3. Resume: for each `status='running'` run with fresh heartbeat, call `Runtime.Resume(ctx, runID)`.
4. Per registered repo: create a `Runtime` and a `gitops.Client`.
5. Per repo, per sensor instance: spawn a `Scheduler` goroutine.
6. Dispatch loop: drain `trigger_queue` rows. For each: fetch the constellation, the nebula, fire a run, mark the trigger dispatched.
7. Loop: step active runs; persist state; honor terminal transitions.

### CLI

`quasar supervise [--config <path>]` starts the supervisor. Designed to be the entrypoint for a systemd unit on the EC2 host.

Heartbeat: the supervisor writes its PID + a heartbeat timestamp to a `supervisor_state` row every 10s. Multiple instances detected by stale PIDs in the same SQLite get an error and refuse to start (single-instance guard via SQLite UNIQUE + advisory lock).

## Files

- `internal/constellations/runtime.go` (new) — Runtime, Fire, Step, Resume
- `internal/constellations/runtime_test.go` (new) — DAG walker, state persistence, resume after crash
- `internal/constellations/state.go` (new) — State struct, marshal/unmarshal
- `internal/constellations/builtins.go` (new) — operator registry
- `internal/constellations/builtins_test.go` (new) — operator dispatch tests
- `internal/constellations/operators/render_seed_prompt.go` (new)
- `internal/constellations/operators/persist_phases.go` (new)
- `internal/constellations/operators/gh_open_pr.go` (new)
- `internal/constellations/operators/verify.go` (new) — verify_test/lint/build operators
- `internal/constellations/operators/notify_human.go` (new)
- `internal/constellations/operators/*_test.go` (new) — per-operator unit tests
- `internal/fabric/constellation_store.go` (new) — ConstellationRunStore typed API
- `internal/fabric/constellation_store_test.go` (new)
- `internal/fabric/migrations/NNN_constellation_runs.sql` (new)
- `internal/supervisor/supervisor.go` (new) — Supervisor type with Run dispatch loop + reaper + resume
- `internal/supervisor/supervisor_test.go` (new) — fake-runtime + fake-scheduler integration tests
- `cmd/supervise.go` (new) — `quasar supervise` entrypoint
- `cmd/supervise_test.go` (new)

## Acceptance Criteria

- [ ] `internal/constellations/` package compiles
- [ ] `constellation_runs`, `star_invocations`, `trigger_queue` tables exist with proper cascades
- [ ] `Runtime.Fire(constellation, nebulaID)` snapshots the constellation TOML into `constellation_snapshot` and inserts a run row
- [ ] `Runtime.Step(runID)` advances one node, evaluates outgoing edges via the expression evaluator, transitions to the matched `to` node, persists `dag_state_toml`
- [ ] `Runtime.Resume(runID)` restores State from `dag_state_toml` and continues from `current_node_id`
- [ ] `star` node type: invokes `agent.Invoker.Invoke(...)` with the resolved star prompt (body + skill fragments) and budget limits from the star's defaults
- [ ] `constellation` node type: creates a child run with `parent_run_id` set; parent waits (via dispatch loop, not blocking) for child terminal status
- [ ] `phase_iterator` node type: fires one sub-constellation per phase in `nebula.Phases`; respects `inputs.parallel` cap
- [ ] `builtin` node type: dispatches to the named operator
- [ ] Operators `render_seed_prompt`, `persist_phases`, `gh_open_pr`, `verify_test`, `verify_lint`, `verify_build`, `notify_human` are registered and pass their unit tests
- [ ] When a star commits via `runtime.gitops.Commit`, the configured `[pre_commit]` from the repo's `.quasar.yaml` runs first; pre-commit failure with `fail_on_error=true` blocks the commit
- [ ] When a pre-commit modifies files, `git add -u` re-stages before the commit
- [ ] Supervisor boots all per-repo runtimes and per-(repo,sensor) schedulers
- [ ] Supervisor reaper marks crashed runs (status='running' but no heartbeat for 60s) as `crashed`
- [ ] Supervisor resume restores in-flight runs with fresh heartbeats
- [ ] Supervisor single-instance guard: starting a second `quasar supervise` against the same SQLite errors out
- [ ] `quasar supervise` runs to completion when SIGTERM is received (graceful shutdown: pause schedulers, flush in-flight state, exit)
- [ ] Sensor backpressure: per-(repo, sensor) `max_inflight` is honored; excess triggers queue
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all exit 0
