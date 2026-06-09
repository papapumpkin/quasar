# Constellation Runtime — Delivered Slice & Tracked Follow-up

> **Mostly superseded — one live tracker remains.** The runtime slice described
> here has shipped and is now documented in [runtime.md](runtime.md) and
> [constellations.md](constellations.md). This log is retained because it is
> still the source of truth for the one deferred item: wiring `cfg.MergeGate`
> (the `[merge_gate]` block in [configuration.md](configuration.md)) into the
> merge-gate constellation's inputs via the merge-gate-firing supervisor. When
> that lands, this document can be retired. Treat the "Delivered" section below
> as historical and the deferred rows as the remaining work.

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

## coder-reviewer-as-inner-constellation phase — delivered & deferred

This phase moved the inner coder-reviewer pair into the `coder-reviewer`
constellation as a real, executable loop and added the operator that drives it.

**Delivered (green):**

- `internal/artifacts/defaults/constellations/coder-reviewer.toml` is now the
  final loop, not the Phase-6 stub: `implement` (coder) → `commit` →
  `review` (reviewer) → `decide` (`reviewer_decision`) → either `_done` (approve),
  a `decide → implement` back-edge (request_changes within cap), or
  `give-up → _failed` (cap exhausted). The cap is declarative in `[meta].max_cycles`
  and enforced by the same back-edge cycle counter as master-review — no Go
  constant, no special case. The stub's bare `review.approved`/`commit.committed`
  guards (which `ExprState` never populated, so they were inert) are replaced with
  the correct `nodes.<id>.<field>` form.
- `internal/constellations/reviewer_decision.go` already existed; this phase added
  its test (`reviewer_decision_test.go`) and put it to use. The operator parses the
  reviewer star's `reviewer-decision-v1` JSON into `{verdict, approved, comments}`.
- `internal/artifacts/defaults/stars/reviewer.md` now emits the
  `reviewer-decision-v1` JSON the operator consumes, mirroring the established
  `master-reviewer-star.md` JSON contract (nothing else consumed the reviewer
  star's former free-text output).
- `coder_reviewer_integration_test.go` drives the embedded constellation
  end-to-end: request-changes-then-approve (two coder invocations → `_done`) and
  always-request-changes (cap exhausted → `_failed` with structured reason).

**Deferred / superseded (same blockers as the cycle-limit phase above):**

| Written criterion | Resolution | Rationale |
|-------------------|------------|-----------|
| `internal/loop/` deleted; `internal/runner/runner.go` < 50 LOC thin `runtime.Fire` shim | **Deferred** | Same blocker as the cycle-limit phase: the runtime cannot yet execute a `NodeConstellation`, so `master-review` cannot call `coder-reviewer` as a child. `internal/loop` is still the only working coder-reviewer for `quasar run` (`cmd/run.go` → `buildLoop`); deleting it would break the build and all `quasar run` CLI tests. The package layout also differs from the phase's assumptions: the engine is `internal/constellations` (not `internal/runtime`) and there is no `runtime.Open`/`Overrides`/`RunResult` API of the shape the shim assumes. Revisit when `NodeConstellation` lands. |
| `CLAUDE.md` rewritten to an `internal/runner` + `internal/runtime` + `internal/stars` structure | **Superseded** | Those directories do not exist; writing that block would be false documentation. `CLAUDE.md`'s structure section was instead updated to reflect the real packages (`constellations/`, `artifacts/`, and the still-present `loop/`). |

## merge-gate phase — delivered & deferred

The `merge-gate` phase adds the cross-phase merge gate: a primitive that merges a
phase's branch into its parent in a throwaway worktree and classifies the
outcome, the operators that surface it to a constellation, and the routing DAG.

**Delivered this cycle (green, tested):**

- `internal/gitops/merge_attempt.go` — `MergeAttempt.Try` merges `SrcBranch` into
  `DstBranch` in a detached worktree under `<git-common-dir>/quasar-merge/` (the
  main working tree is never touched) and classifies `clean` / `markers` /
  `build_failure` / `merge_error`. `TryOpts.Timeout` caps the verify command
  (`context.WithTimeout`); `KeepWorktree` controls cleanup. Fixture-repo tests
  cover every outcome plus cleanup and the untouched-main-tree invariant.
- `internal/constellations/operators_merge.go` — the `merge_attempt` and
  `fulfill_entanglements` builtins. `merge_attempt` reads `src_branch`,
  `dst_branch`, and the optional `verify_command` / `verify_timeout` / `run_id`
  inputs, keeps the worktree only for resolver-hand-off outcomes, and emits the
  documented output schema. `fulfill_entanglements` transitions a producing run's
  in-flight entanglements once given its `run_id`.
- `internal/artifacts/defaults/constellations/merge-gate.toml` — routes each
  `MergeResult` to the documented next node; `internal/artifacts/defaults/
  constellations/merge-conflict-resolve.toml` ships as a conservative
  escalate-to-human placeholder (Phase 03 replaces it).
- `internal/config/config.go` — the `[merge_gate]` block (`verify_command`,
  `verify_timeout`) as the per-repo landing surface, loaded and tested.

**Deferred to the merge-gate-firing supervisor follow-on (tracked, not dropped):**

| Acceptance criterion | Status | Notes for follow-up |
|----------------------|--------|---------------------|
| Supervisor fires `merge-gate` after a phase's primary run reaches `_done`, and does not mark the phase fulfilled until the gate reaches `_done` | **Deferred** | There is no supervisor / `internal/runtime/supervisor.go` yet (the engine is `internal/constellations`; per-phase dispatch via `phase_iterator` is still `ErrNodeTypeUnsupported`). Same dependency as the supervisor rows above. The primitive + operators + DAG are ready for it to drive. |
| `fulfill_entanglements` receives the producing run's `run_id` (the `fulfill` nodes in `merge-gate.toml` pass only `merged_sha` today) | **Deferred** | State carries no run id in the expression namespace, so the firing supervisor must thread the producing `run_id` into both `fulfill` inputs. `opFulfillEntanglements` already accepts and acts on `run_id`; until supplied it is a no-op (`fulfilled=false`). |
| Per-repo `merge_gate.verify_command` / `verify_timeout` override takes effect end-to-end | **Partial** | The primitive (`TryOpts.VerifyCommand`/`Timeout`) and operator (`verify_command`/`verify_timeout` inputs) honor overrides and are tested. The missing link is config → constellation input, whose natural injection point is the merge-gate-firing supervisor; `cfg.MergeGate` has no consumer until then. |

## Sequencing note

The deferred items are not independent: `constellation`/`phase_iterator`
dispatch, the reaper/resume wiring, and the `trigger_queue` drain loop all hang
off the **supervisor**. The natural follow-up phase is "constellation
supervisor": build `internal/supervisor/` + `cmd/supervise.go` first, then wire
sub-DAG dispatch and backpressure through it. `gh_open_pr` is gated separately
on the `Forge` write nebula.
