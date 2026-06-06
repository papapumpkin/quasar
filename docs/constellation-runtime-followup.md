# Constellation Runtime — Delivered Slice & Tracked Follow-up

The `constellation-runtime` phase is the "load-bearing" phase: the engine that
executes constellations. It is genuinely large. This cycle delivered a
**coherent, independently-valuable, fully-tested runtime slice**; the remaining
acceptance criteria are **explicitly tracked here as a follow-up phase** rather
than silently dropped.

This document is the source of truth for what is done vs. deferred. When the
follow-up phase lands, delete the corresponding rows.

## Delivered this cycle (green, tested)

- `internal/constellations/` package: `State` machine (`state.go`), DAG walker
  and edge-guard evaluation (`walk.go`), `Runtime.Fire`/`Step`/`Resume`
  (`runtime.go`).
- `star` node dispatch: invokes `agent.Invoker.Invoke` with the resolved star
  prompt and per-star budget/effort defaults; records a `star_invocation` row.
- `builtin` node dispatch: operator registry (`builtins.go`) + operators
  `render_seed_prompt`, `persist_phases`, `commit`, `notify_human`,
  `verify_test`/`verify_lint`/`verify_build` (`operators.go`).
- Pre-commit threading: the `commit` builtin is the sole commit site; the
  runtime threads the repo's `[pre_commit]` into every `gitops.Commit`. Stars
  never commit (see "Stars and git writes" in `docs/safety.md`).
- Crash-safe resume: `dag_state_toml` persisted after every transition;
  `Resume` restores `State` and continues from `current_node`.
- Storage: `internal/fabric/constellation_store.go` (`ConstellationRunStore`
  typed API incl. `ReapStale`, `Heartbeat`, `ListByState`) + migration
  `internal/fabric/migrations/004_constellation_runtime.sql` (additive columns
  over the `003` runtime tables).

## Deferred to follow-up phase(s)

| Acceptance criterion | Status | Notes for follow-up |
|----------------------|--------|---------------------|
| `constellation` node type (child run with `parent_run_id`, parent waits via dispatch loop) | **Deferred** | Currently returns `ErrNodeTypeUnsupported` in `dispatch` (`runtime.go`). Needs the supervisor dispatch loop to drive child runs. |
| `phase_iterator` node type (one sub-constellation per `nebula.Phases`, `inputs.parallel` cap) | **Deferred** | Same dependency on the dispatch loop; `ReapStale`/`ListByState`/parent-run columns already exist to support it. |
| `internal/supervisor/` — boot, per-repo `Runtime` + `gitops.Client`, per-(repo,sensor) schedulers | **Deferred** | Store-side primitives (`ListByState`, `ReapStale`, `Heartbeat`) are in place for it to drive. |
| Supervisor reaper (mark `status='running'` with stale heartbeat as `crashed`) | **Partial** | `ConstellationRunStore.ReapStale` exists and is tested; nothing calls it yet. |
| Supervisor boot-time resume of in-flight runs | **Partial** | `Runtime.Resume` exists and is tested; the supervisor that calls it on boot does not. |
| Single-instance guard + heartbeat + SIGTERM graceful shutdown | **Deferred** | Needs `supervisor_state` row (PID + heartbeat) and signal handling. |
| `trigger_queue` typed API + dispatch loop + sensor backpressure (`max_inflight`) | **Deferred** | `trigger_queue` table exists (migrations `003`/`004`); the drain loop and per-(repo,sensor) inflight cap are not wired. |
| `cmd/supervise.go` — `quasar supervise` entrypoint | **Deferred** | systemd-unit entrypoint; depends on the supervisor type. |
| `gh_open_pr` operator | **Deferred** | Only the constellation default (`internal/artifacts/defaults/constellations/open-pr.toml`) references it. Blocked on the `Forge` write methods, which the project constraints place in a later nebula. |
| Rationale blobs written to `star_invocations.rationale_blob_hash`/`rationale_preview` | **Deferred** | Columns exist (migration `004`); `recordInvocation` does not yet write the LLM reasoning blob. |

## Cycle-limit phase — ratified deviations from the written criteria

The `cycle-limit-in-constellation` phase delivered the declarative cycle cap
(`master-review.toml [meta].max_cycles`, per-run override at Fire time, back-edge
cycle counting, give-up→`_failed` with a structured reason — all tested in
`internal/constellations`). Two of its written acceptance bullets were **not**
implemented as literally specified because they contradict the same phase's
stated constraints and the current engine reality. These deviations are
**ratified here** (reviewer-recommended Option A) rather than left as silently
unchecked boxes.

| Written criterion | Resolution | Rationale |
|-------------------|------------|-----------|
| `internal/loop/` is renamed to `internal/runner/` and reduced to a Fire shim | **Superseded / deferred** | Directly contradicts the phase constraint "internal/loop continues to exist for in-process invocation … but is no longer the master-review owner." Gutting it now would disable the only working coder-reviewer implementation, since the runtime cannot yet execute an inner constellation node (`NodeConstellation` → `ErrNodeTypeUnsupported`). Revisit when that node type lands and a runtime replacement exists. |
| `CycleGuard` + `cycle_counts` BLOB column + migration (`internal/runtime/cycle_guard.go`, `NNN_cycle_counts.sql`) | **Superseded** | Violates the phase constraint "No new abstractions for budget tracking." The single per-run `State.Cycle` (persisted to the existing `constellation_runs.cycle` column from migration `004`) already satisfies "cycle counting is per-run" without a parallel per-node state model. Per-node `cycle_counts` granularity is only needed once multiple independent loops exist. |
| "No hardcoded cycle constants remain in Go" — `internal/nebula/config.go` `DefaultMaxReviewCycles = 3` | **Kept (defensible)** | This constant is the fallback default for the per-run **override knob** (`[execution].max_review_cycles`), consumed by `ResolveExecution` — not the master-review **cap**, which is declarative in TOML. It passes the literal arch-grep (`maxCycles\s*=\s*\d+`) and has live usages + tests; removing it would break `internal/nebula`. |
| Real master-review back-edge (within-cap `fix` loops into the inner coder-reviewer) | **Deferred** | Blocked on `NodeConstellation` support (same dependency as above). Until then the embedded `master-review.toml` routes a within-cap `fix` to `_awaiting_human` (never `_done`), so a needs-changes run is never marked successful. The runtime's cycle-counting + give-up path is proven by the looping fixture test. |

## Sequencing note

The deferred items are not independent: `constellation`/`phase_iterator`
dispatch, the reaper/resume wiring, and the `trigger_queue` drain loop all hang
off the **supervisor**. The natural follow-up phase is "constellation
supervisor": build `internal/supervisor/` + `cmd/supervise.go` first, then wire
sub-DAG dispatch and backpressure through it. `gh_open_pr` is gated separately
on the `Forge` write nebula.
