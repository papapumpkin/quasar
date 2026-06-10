# Runtime — the constellation engine

The **constellation runtime** (`internal/constellations/`) is the engine that
walks a constellation DAG against a nebula: it dispatches each node, evaluates
edge guards, enforces the budget, and persists state after every transition for
crash-safe resume. It is deliberately thin — it wires together primitives that
already exist (the expression language, `agent.Invoker`, `gitops.Client`,
`fabric` stores) rather than reimplementing them (`state.go:1-12`).

Read [constellations.md](constellations.md) first for the declarative surface
(nodes, edges, the expression language). This document is the *execution* side:
the structs, the Fire → Step → terminate walk, nested dispatch, budget, the
supervisor, the dead-coder healthcheck, and prompt-cache telemetry. State storage
is in [fabric.md](fabric.md); the git/safety perimeter is in [safety.md](safety.md).
Three subsystems built on this engine have their own docs:
[multi-repo.md](multi-repo.md) (the supervisor + step driver that drive Fire and
Step across repos), [entanglements.md](entanglements.md) (the symbol-lifecycle
hooks the runtime fires), and [conflict-resolution.md](conflict-resolution.md)
(the merge gate and nested conflict-resolver constellation).

---

## 1. The Runtime struct + RuntimeOpts

`Runtime` (`internal/constellations/runtime.go:60-76`) executes runs for a
**single repo**; the supervisor owns one per registered repo. Its fields:

| Field | Purpose | Omit cost |
|---|---|---|
| `runStore *fabric.ConstellationRunStore` | persists run rows + DAG state | required (panics, `runtime.go:137-138`) |
| `nebStore *fabric.NebulaStore` | loads the nebula snapshot at Fire | required |
| `loader Loader` | resolves constellations + stars | required |
| `invoker agent.Invoker` | the LLM seam for star nodes | star nodes error without it (`dispatch_star.go:31-32`) |
| `committer Committer` | the git-write seam (commit + diff) | commit builtin errors (`runtime.go:322-323`) |
| `repoPath string` | working dir for git ops | merge/commit ops need it |
| `preCommit gitops.PreCommitConfig` | pre-commit gate, threaded into every commit | commits skip the gate |
| `budget *Budget` | per-run USD cap | constructed from `runStore` (`runtime.go:148`) |
| `defaultBudgetUSD float64` | fallback cap when nebula/override absent | uncapped fallback |
| `cacheMetrics *telemetry.CacheMetricStore` | prompt-cache token JSONL | no cache telemetry (`runtime.go:70`) |
| `checkpointer Checkpointer` | per-dispatch worktree snapshots | no dead-coder fallback (`runtime.go:71`) |
| `entanglements *fabric.EntanglementStore` | symbol lifecycle | no cross-phase coordination (`runtime.go:72`) |
| `merger merger` | test seam for merge attempts | builds gitops-backed merger (`runtime.go:73`) |
| `coordination *Check` | pre-flight coordination notes | dispatch proceeds with no notes (`runtime.go:74`) |
| `conflictLog *telemetry.ConflictResolutionLog` | conflict-resolution outcomes | emit node no-ops (`runtime.go:75`) |

`RuntimeOpts` (`runtime.go:99-132`) is the public constructor input; `New`
(`runtime.go:136-156`) panics on a nil required dependency so a wiring bug
surfaces at boot rather than as a mid-run nil dereference. The optional fields
above are each documented inline as "nil disables X" — a repo that doesn't
coordinate symbols or record telemetry pays nothing.

---

## 2. Fire — instantiate a run

`Fire` (`runtime.go:164-208`) creates a run row positioned at the entry node and
returns its `run_id`. Execution is asynchronous: the supervisor then drives
`Step`. Steps:

1. **Load the constellation** — `r.loader.LoadConstellation` (`runtime.go:165`).
2. **Find the entry node** — `entryNode` (`runtime.go:169`), the first node that
   is no edge's target (`walk.go:14-28`).
3. **Load the nebula** — `r.nebStore.Get` (`runtime.go:174`).
4. **Build initial State** — `NewState(SnapshotNebula(neb), now)` (`runtime.go:180`)
   denormalizes the nebula into the evaluation context (`state.go:96-127`).
5. **Resolve the cycle cap** — `resolveMaxCycles(con, neb)` onto
   `st.Meta.MaxCycles` (`runtime.go:181`).
6. **Marshal State** to the `dag_state_toml` column value — `MarshalState`
   (`runtime.go:182`).
7. **Insert the run row** — `r.runStore.InsertRun` with repo, nebula,
   constellation, snapshot, parent run, `StateRunning`, entry node, and the DAG
   state (`runtime.go:187-196`).
8. **Initialize the budget** — `r.budget.Initialize(ctx, runID,
   resolveBudget(...))` (`runtime.go:204`); an uncapped result leaves the budget
   columns NULL so `CheckBefore` no-ops.
9. **Return `run_id`** (`runtime.go:207`).

---

## 3. Step — advance one node

`Step` (`runtime.go:214-263`) advances a run by exactly one node and returns the
run's new state; calling it on a terminal run returns `ErrTerminal`
(`runtime.go:219-221`). The flow:

1. **Load the run** and reject terminals (`runtime.go:215-221`).
2. **Load the constellation; unmarshal State** from `dag_state_toml`
   (`runtime.go:223-230`) — restoring exactly where the prior Step left off.
3. **Find the current node** (`findNode`, `runtime.go:231`); a missing node fails
   the run with `ErrUnknownNode` (`runtime.go:232-234`) — the guard against a
   corrupt or edited constellation.
4. **Dispatch** via the type switch (`r.dispatch`, `runtime.go:236`,
   switch at `runtime.go:283-303`). A `ErrBudgetExhausted` return routes to
   `failBudget` (`runtime.go:238-240`); any other error fails the run
   (`runtime.go:241`).
5. **Record the node's outputs** into `State.Nodes[nodeID]`
   (`st.RecordNode`, `runtime.go:243`, `state.go:133-141`).
6. **Evaluate outgoing edges** — `nextTarget` returns the `to` of the first
   truthy `when` guard (`runtime.go:245`, `walk.go:84-106`).
7. **Back-edge detection + cycle increment** — `isBackEdge(con, node.ID, next)`
   then `st.Cycle++` (`runtime.go:255-257`); see [§6](#6-cycle-counting-and-back-edges).
8. **Persist** — a terminal target calls `terminate` (`runtime.go:259-261`),
   otherwise `persistTransition` saves the new node + DAG state via `SaveProgress`
   (`runtime.go:262`, `persistTransition` at `runtime.go:336-350`).

### Fire → Step → terminate, happy path (a coder-reviewer phase)

1. `Fire("coder-reviewer", …)` inserts a run at node `implement`
   (`runtime.go:187`).
2. `Step` dispatches `implement` (star: coder), records
   `{result, cost_usd, session_id}` (`dispatch_star.go:106-110`), edges
   unconditionally to `commit` (`coder-reviewer.toml:59-61`), persists.
3. `Step` dispatches `commit` (builtin) — the runtime writes the commit through
   `commitWork` (`runtime.go:321-333`) and marks touched symbols in-flight
   (`runtime.go:287-292`); edges to `review` when `nodes.commit.committed`
   (`coder-reviewer.toml:63-66`).
4. `Step` dispatches `review` (star: reviewer); edges unconditionally to `decide`.
5. `Step` dispatches `decide` (builtin `reviewer_decision`). If
   `nodes.decide.approved`, `nextTarget` returns `_done` (`coder-reviewer.toml:72-75`);
   `IsTerminal("_done")` is true so `terminate` maps it to `StateDone`, saves
   final State, calls `Complete`, and applies terminal entanglements
   (`runtime.go:353-377`).

A `request_changes` verdict instead routes back to `implement` (a back-edge,
incrementing `cycle`) until either approval or `cycle >= meta.max_cycles`, which
routes to `give-up` → `_failed`.

---

## 4. dispatchStar — the safety invariant

`dispatchStar` (`internal/constellations/dispatch_star.go:30-111`) resolves the
star, invokes the LLM, accumulates cost, and records a `star_invocation` row.

> **SAFETY INVARIANT** (`dispatch_star.go:21-29`): stars must never be granted
> git-write tools. A star edits the worktree; the *commit* happens only in the
> `commit` builtin node, the sole place the runtime threads the repo's
> `[pre_commit]` config into `gitops.Commit`. A star with direct git access could
> commit inside the worktree, bypassing both the `internal/gitops` perimeter and
> the pre-commit gate. The runtime does not yet enforce this at load time (a
> loader-side rejection is a follow-up); until then it is an authoring rule
> backed by each star's `denied` tool list (see [safety.md](safety.md)).

Walkthrough:

1. **Pre-flight budget gate** — `r.budget.CheckBefore(ctx, run.ID)`
   (`dispatch_star.go:36`); the `ErrBudgetExhausted` sentinel propagates to
   `Step`, which routes to `failBudget`.
2. **Load the star** (`dispatch_star.go:39`) — flattened `Tools.Allowed`.
3. **Evaluate inputs** (`dispatch_star.go:43`).
4. **Coordination notes** — `r.coordinationPrompt(...)` appends a
   `## Coordination notes` block when sibling intents intersect the phase
   (`dispatch_star.go:53`, `coordinationPrompt` at `dispatch_star.go:134-147`).
   Advisory only — a read failure is logged and swallowed. *Known limitation:
   this produces zero notes in the runtime today, by design for this phase
   (`dispatch_star.go:119-133`).*
5. **Invoke claude** — `r.invoker.Invoke` with `CacheOptimization: true`, the
   star's context budget, and its health policy (`dispatch_star.go:56-75`).
6. **Dead-coder handling** — a `*claude.DeadCoderError` is recorded as
   `terminated_health` and `restoreForReview` materializes the latest snapshot
   beside the partial worktree (`dispatch_star.go:80-92`, `restoreForReview` at
   `dispatch_star.go:178-189`).
7. **Charge budget atomically** — `r.budget.RecordCost` inserts the invocation
   trace *and* decrements the remaining budget in one transaction
   (`dispatch_star.go:95-103`).
8. **Record cache metric**, then **checkpoint** the worktree
   (`dispatch_star.go:104-105`).

---

## 5. dispatchConstellation — nested runs

`dispatchConstellation` (`internal/constellations/dispatch_constellation.go:26-98`)
is the 2026-06-08 deliverable that lets master-review call coder-reviewer as a
real inner loop. It runs a child constellation referenced by `node.Ref` to
completion and projects the child's `[outputs]` back to the parent. It is
**synchronous**: the parent's `Step` blocks until the child is terminal.

1. **Input seeding** — evaluate the parent node's inputs (`dispatch_constellation.go:30`),
   `Fire` the child against the same nebula with the parent run as its parent
   (`dispatch_constellation.go:34`), then `seedChildInputs` writes those inputs into
   the child's `State.Inputs` and re-persists (`dispatch_constellation.go:42-46`,
   `seedChildInputs` at `dispatch_constellation.go:103-124`).
2. **Drive to terminal** — loop `r.Step(childRunID)` until `ErrTerminal` or a
   terminal state (`dispatch_constellation.go:53-64`); back-edges increment the
   *child's own* cycle counter.
3. **Output projection** — re-load the child run + State, evaluate the child
   constellation's `[outputs]` against the child's final state, and return a map
   of `{state, run_id, <child outputs>}` (`dispatch_constellation.go:69-93`). A
   child that ended `failed` is propagated as a parent-node error so the parent's
   failure-path edges fire (`dispatch_constellation.go:94-96`).

---

## 6. Cycle counting and back-edges

The runtime advances a run's cycle counter solely on **back-edges** — a
transition to a node declared at or before the source. `isBackEdge`
(`internal/constellations/walk.go:69-78`) compares declaration indices:
`toIdx <= fromIdx` (`walk.go:77`), exempting terminal targets (`walk.go:70`).
Each back-edge does `st.Cycle++` in `Step` (`runtime.go:255-257`), and the new
count persists with the transition. Because guards read `cycle` and
`meta.max_cycles`, the declarative cap bites with **no hardcoded Go constant**.

`meta.max_cycles` is resolved at Fire from the constellation's `[meta]` plus a
per-run override (a nebula's `[execution].max_review_cycles`) onto
`st.Meta.MaxCycles` (`runtime.go:181`, surfaced by `state.go:184-188`).

> **Limitation** (`walk.go:62-68`): cycle topology is inferred *positionally*. A
> legitimate forward edge into an earlier-declared join node would be miscounted.
> This is correct for the single-loop constellations shipped today; explicit
> loop markers are a tracked follow-up.

---

## 7. Budget enforcement

`Budget` (`internal/constellations/budget.go:32-44`) reuses the
`star_invocations` cost columns rather than a separate ledger:

- **Initialize** at Fire (`budget.go:48-53`); a non-positive amount selects
  no-cap mode (NULL columns).
- **CheckBefore** each star invocation returns `ErrBudgetExhausted`
  (`budget.go:21`) when a capped run is spent (`budget.go:58-67`).
- **RecordCost** decrements the remaining budget in the same SQL transaction that
  inserts the invocation trace, so a crash between the two writes can neither
  double-charge nor skip the cost (`budget.go:69-75`, backed by
  `RecordInvocationCost` at `constellation_store.go:286`).
- **failBudget** (`budget.go:142-158`) records a structured `_error` node with a
  per-node cost breakdown (`budgetDetail`, `budget.go:112-136`) and marks the run
  failed. Precedence for the cap (`resolveBudget`, `budget.go:163-176`): explicit
  override > nebula `[execution].max_budget_usd` > global default.

---

## 8. State persistence

`State` (`internal/constellations/state.go:28-42`) is what edge guards evaluate
against. Its fields: `Inputs`, `Nodes` (each completed node's outputs by ID),
`Nebula` (a denormalized `NebulaSnapshot`), `Cycle`, and `Meta` (cost, start
time, `MaxCycles`). After every transition it is serialized to the
`dag_state_toml` column by `MarshalState` (`state.go:203-209`) and restored by
`UnmarshalState` (`state.go:212-224`).

Because the current node and full evaluation context live in the row, **resume is
trivial**: `Resume` (`runtime.go:268-280`) just unmarshals the DAG state to detect
corruption and refreshes the heartbeat; the supervisor then drives `Step` from
the persisted node. No in-memory walker state is ever load-bearing. The TOML
shape (`Inputs`, `Nodes`, `Nebula`, `Cycle`, `Meta`) is detailed in
[fabric.md §6](fabric.md#6-statetoml-serialization).

---

## 9. The supervisor

`Supervisor` (`internal/constellations/supervisor.go:53-65`) drains
`trigger_queue` rows by firing a constellation run for each pending entry —
without it, the fleet view's Approve action and every sensor trigger are silent
no-ops. `Tick` (`supervisor.go:80-109`) selects up to `BatchLimit` pending rows
(default 8, `supervisor.go:15`), **claims** each via an atomic
`UPDATE … WHERE state='pending'` (`supervisor.go:172-183`) — so concurrent ticks
can't double-fire — then calls `Firer.Fire`. The claim precedes Fire so a crash
mid-Fire is auditable (consumed-without-run) rather than leaking double runs
(`supervisor.go:74-79`). A Fire that fails still marks the row consumed and logs,
rather than retrying the same failure forever (`supervisor.go:67-72`). `Run`
(`supervisor.go:114-130`) drives `Tick` on an interval until cancel.

The **`Firer`** interface (`supervisor.go:25-27`) is the seam onto whatever
materializes a run. `*Runtime` is bound to one repo and doesn't satisfy it
directly:

- **`SingleRepoFirer`** (`supervisor.go:32-42`) wraps one `*Runtime`, ignoring the
  trigger's `repoPath` — for tests and single-repo deployments.
- **`RuntimeCacheFirer`** (`runtime_cache.go:137-150`) routes each call through a
  **`RuntimeCache`** (`runtime_cache.go:51-80`) that lazily constructs and caches
  one `*Runtime` per repo path (`RuntimeCache.Get`, `runtime_cache.go:86-131`),
  binding per-repo working dir, gitops client, loader, and pre-commit policy while
  sharing the DB, blob store, and invoker.

The `fleet` command wires this together: `startTriggerSupervisor`
(`cmd/fleet.go:108`) builds a `RuntimeCache` (`cmd/fleet.go:124`) and a
`Supervisor` with a `RuntimeCacheFirer` (`cmd/fleet.go:136-138`), routing
diagnostics to `.quasar/supervisor.log` (`cmd/fleet.go:176`) so they never
collide with the Bubble Tea TUI on stderr.

---

## 10. The healthcheck and dead-coder detection

A coder runs inside an opaque `claude -p` subprocess; the healthcheck
(`internal/claude/healthcheck.go`) keeps a single invocation from burning an hour
silently. It samples multiple signals (`healthcheck.go:45-50`):

- `signalWallClock` — absolute lifetime cap;
- `signalWriteIdle` — longest quiet stretch under the workdir;
- `signalTokenRate` — stream output tokens/sec floor;
- `signalToolRatio` — Read:Edit call ratio ceiling;
- `signalCPUIdle` — longest sub-1%-CPU stretch.

`evaluateHealth` (`healthcheck.go:88`) classifies a snapshot into a `HealthState`
(`healthcheck.go:17-27`): `Healthy`, `Degraded` (one red signal — operator-visible,
recoverable), or `Dead` (two or more red, or the wall-clock cap). `Run`
(`healthcheck.go:205`) ticks on an interval; a transition to `Dead` calls
`terminate` (`healthcheck.go:265`), which records telemetry and runs
`killWithGrace` (`healthcheck.go:302`) — SIGTERM, a grace window, then SIGKILL —
returning a `*claude.DeadCoderError`. The star defaults (`coder.md:50-54`):
25-minute wall clock, 5-minute write-idle, token-rate floor 5, 90-second
CPU-idle. The partial worktree survives, so `dispatchStar` hands it plus the
latest checkpoint to the reviewer (see [§4](#4-dispatchstar--the-safety-invariant)).

---

## 11. In-cycle checkpointing

When a `Checkpointer` is wired and the star opts in via `[checkpoint] enabled`,
`maybeCheckpoint` (`dispatch_star.go:164-172`) snapshots the worktree after each
successful coder dispatch, labeled `post-dispatch:<star>`. Granularity is
**per-dispatch, not per-build** (`dispatch_star.go:152-158`): the runtime cannot
observe the coder's individual tool calls inside the subprocess, so a coder that
returned without a `DeadCoderError` left a usable tree the *next* cycle's dead
coder can fall back to via `restoreForReview` (`dispatch_star.go:178-189`).
Snapshots are best-effort — a failure is logged, never fatal. The snapshot
contents and blob-backed storage are in [fabric.md §3](fabric.md#3-tables)
(`checkpoints` / `checkpoint_files`).

---

## 12. Prompt cache + telemetry

Star prompts are laid out in two zones (`internal/agent/prompt_layout.go:14-44`):
`ZoneStablePrefix` (the system prompt — byte-identical across firings of a node,
so the Anthropic prompt cache hits) and `ZoneVolatileSuffix` (the per-cycle user
prompt). `ContentZone` (`prompt_layout.go:77`) maps well-known content labels to
a zone, and `PromptManifest` (`prompt_layout.go:94-112`) hashes the system prompt
for verification.

To keep the system prompt byte-stable, the invoker passes
`--exclude-dynamic-system-prompt-sections` whenever `a.CacheOptimization` is true
(`internal/claude/claude.go:98-99`, inside `buildArgs` at `claude.go:86`); a
star's dispatch always sets `CacheOptimization: true` (`dispatch_star.go:66`).

Per-invocation cache-token counts are appended to a JSONL log via
`CacheMetricStore.Record` (`internal/telemetry/cache_metrics.go:78`), called from
`recordCacheMetric` (`dispatch_star.go:237-252`). Recording is best-effort — a
write failure is logged but never fails the step, since telemetry is a read-only
side channel that must not block the walk. The records feed `quasar cache report`
(`CacheMetric` at `cache_metrics.go:21-31`).
