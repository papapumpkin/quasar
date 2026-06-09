# Quasar Output Safety Perimeter

Quasar runs autonomous agents that write code, commit it, push branches, and
open pull requests on your behalf. This document describes the **output safety
perimeter**: the boundary that constrains what those agents can do to your
repository and your forge, and exactly how that boundary is enforced in the
current codebase.

The perimeter is real and load-bearing today. Every git *write* is routed
through one package (`internal/gitops/`), an architecture test fails CI if any
other package shells out to git, and the agents (stars) are structurally denied
git-write tools. No single layer is trusted alone.

For where each piece lives in the broader system, see
[architecture.md](architecture.md); for the runtime that drives commits and
pushes, see [runtime.md](runtime.md).

## What Quasar can do

The allowlist — everything Quasar is permitted to do to your repo and forge:

- **Push to `quasar/*` branches it owns.** Every branch Quasar creates is
  namespaced under `quasar/`. The push wrapper refuses any other target ref
  (`internal/gitops/push.go:57`).
- **Open pull requests** from a `quasar/*` head branch against your base branch,
  via `gh pr create` confined to the forge adapter
  (`internal/forge/forge.go`). It creates PRs; it never merges, closes, or
  edits them.
- **Read everything.** Issues and comments through the GitHub sensor's `gh`
  reads (`internal/sensors/github/`), and file contents through the per-cycle
  worktree. Reads are unrestricted; writes are not.
- **Run `claude` in subprocesses** scoped to a per-cycle git worktree, so an
  agent only ever sees and edits the changes for the run it belongs to.
- **Commit through the runtime's `commit` builtin**, which threads your
  `[pre_commit]` quality gate into every commit (see
  [The pre-commit gate](#the-pre-commit-gate)).

## What Quasar cannot do

The denylist. Each item is enforced by code, not convention:

- **Force-push to any branch.** The only push mode is `--force-with-lease`
  (`internal/gitops/push.go:69`); a stale lease fails safely with
  `ErrForcePushRejected` (`internal/gitops/push.go:19`) without overwriting
  anything. An unconditional `--force` anywhere in `internal/gitops/` is caught
  by the `TestNoForbiddenGitSubcommands` smell test
  (`internal/arch_test/safety_test.go:154`).
- **Push directly to `main`/`master`/the base branch.** Two independent guards:
  the `quasar/*` allowlist regex (`internal/gitops/push.go:23`) and a redundant
  `forbiddenPushBranches` list (`internal/gitops/push.go:29`) that rejects bare
  base-branch names even if the regex were ever bypassed. The redundancy is
  deliberate and **not config-overridable** (`internal/gitops/push.go:25-28`),
  so a misconfiguration can never disable the guard. A bad target returns
  `ErrUnsafeRef` (`internal/gitops/push.go:14`); there is no override flag.
- **Merge pull requests.** `gh pr merge` is in every coder star's denied-tools
  list (`internal/artifacts/defaults/stars/coder.md:14`), and the forge adapter
  only ever runs `gh pr create` (`internal/forge/forge.go`).
- **Delete branches or tags.** `branch -D` and ref-deletion pushes
  (`push origin :…`) are flagged by `forbiddenSubcommandSmell`
  (`internal/arch_test/safety_test.go:157-162`).
- **Commit on behalf of a star.** A star edits the worktree but cannot commit;
  commits flow only through the runtime's `commit` builtin. See
  [Stars and git writes](#stars-and-git-writes--the-safety-invariant).

## The safety perimeter

**`internal/gitops/` is the only package that runs `git` writes.** Centralizing
every write in one wrapper means the ref allowlist, the force-with-lease policy,
and the pre-commit gate are enforced in exactly one place that the rest of the
codebase cannot route around.

This is enforced by an architecture test, not by review discipline alone.
`TestNoDirectGitExecOutsideGitops` (`internal/arch_test/safety_test.go:99`)
parses every non-test `.go` file under `cmd/` and `internal/` and fails the
build if any of them calls `exec.Command("git", …)` outside the perimeter.

Two intentional carve-outs exist:

- **Migration exceptions.** A handful of files predate the perimeter and are
  grandfathered in the `gitExecExceptions` map
  (`internal/arch_test/safety_test.go:30-38`): `internal/loop/git.go`,
  `internal/nebula/git.go`, `internal/nebula/branch.go`,
  `internal/checkpoint/checkpoint.go`, `internal/fabric/publisher.go`,
  `internal/snapshot/scanner.go`, and `internal/tui/bridge.go`. These are
  *known* direct callers; new ones are **not** exempt and will fail the wall.
  The list shrinking to empty is the migration's definition of done
  (`internal/arch_test/safety_test.go:28-29`).
- **A temporary warn gate.** `QUASAR_ARCH_TEST_GIT_WALL=warn`
  (`internal/arch_test/safety_test.go:21`) downgrades violations to log lines so
  a package owner mid-migration can keep a green suite. With the variable unset
  — the CI default — every violation is a hard failure.

The `gh` CLI is confined the same way. `TestNoDirectGHExecOutsideAllowedPackages`
(`internal/arch_test/safety_test.go:121`) permits `exec.Command("gh", …)` only
under the prefixes in `ghExecAllowedPrefixes`
(`internal/arch_test/safety_test.go:45-48`): `internal/sensors/github/` (ticket
reading) and `internal/forge/` (PR creation). Using `gh` for git operations
elsewhere would fork Quasar's forge support, so it is blocked.

Other arch tests in `internal/arch_test/` round out the perimeter — for example,
asserting that committed configs carry no inline tokens (secrets must use
`token_env`/`token_file`). See [the secrets rule below](#token-scopes).

## Stars and git writes — the SAFETY INVARIANT

The runtime keeps git writes on a single rail by **separating editing from
committing**:

- A **star** node invokes the LLM to edit the worktree. It produces a diff; it
  does **not** commit.
- A **`commit` builtin** node is the *only* place a commit is created, and the
  only call site that threads the repo's `[pre_commit]` config into
  `gitops.Commit`.

This yields a hard invariant, documented on `dispatchStar`
(`internal/constellations/dispatch_star.go:21-29`):

> **Stars must never be granted git-write tools.** A star edits the worktree;
> the commit happens only in the `commit` builtin node. If a star's
> allowed-tools included direct git access, the LLM could commit inside the
> worktree itself, bypassing both the `internal/gitops` perimeter and the
> pre-commit gate.

Concretely, a star's `[tools]` block allows edit/read/build tools and **denies**
git-write tools. The default coder star
(`internal/artifacts/defaults/stars/coder.md:12-14`):

```toml
[tools]
allowed = ["Read", "Edit", "Write", "Glob", "Grep", "Bash(go *)", "Bash(git diff *)", "Bash(git status)"]
denied  = ["Bash(git push *)", "Bash(gh pr merge *)", "Bash(git reset *)", "Bash(git commit *)", "Bash(git add *)"]
```

> **Trade-off / current limitation.** Today this is an *authoring rule*: the
> runtime does not yet reject a star whose `[tools].allowed` contains a
> git-write tool (`internal/constellations/dispatch_star.go:27-28`). A
> loader-side rejection lands when the star tool model firms up. Until then,
> authoring a star with `Bash(git commit *)` in `allowed` would breach the
> invariant — so keep stars to edit/read tools and route all commits through the
> `commit` builtin. `quasar lint` validates artifact files and is the place such
> a check will live.

## Worktree isolation per cycle

Each `constellation_run` executes in its **own git worktree** on a dedicated
`quasar/<run-id>` branch, so one run can never see another's uncommitted state.
The worktree reaper only ever considers worktrees whose branch is under
`refs/heads/quasar/` (`internal/gitops/worktree_reaper.go:16`), which means it
can never touch a human's main checkout or an unrelated worktree.

The **merge gate** — which tests whether a finished run's branch merges cleanly
into the base — runs in its *own* detached temporary worktree under the git
common directory, created with `git worktree add --detach`
(`internal/gitops/merge_attempt.go`). It never mutates your checkout; on
completion the temporary worktree is removed.

When a run reaches a terminal state, its worktree becomes reclaimable and the GC
worktree reaper removes it — conservatively, never deleting a worktree whose run
row is still non-terminal. See [gc.md](gc.md#the-worktree-reaper).

## The pre-commit gate

Every commit runs your configured quality gate; there is **no bypass**. The
runtime has exactly one commit call site
(`internal/constellations/runtime.go:325`):

```go
sha, err := r.committer.Commit(ctx, message, gitops.CommitOpts{PreCommit: r.preCommit})
```

`r.preCommit` (`internal/constellations/runtime.go:67`) is the repo's
`[pre_commit]` block, loaded once and threaded into every commit. Because this
is the only place a commit is made, no star and no constellation author has to
know about pre-commit — it is applied uniformly.

`PreCommitConfig` (`internal/gitops/precommit.go:17-26`) mirrors the
`[pre_commit]` block:

- `Commands` — shell strings, each run as `sh -c <cmd>` with the worktree as the
  working directory, in order.
- `FailOnError` — when true, a command exiting non-zero **aborts the commit**
  with `ErrPreCommitFailed` (`internal/gitops/precommit.go:15`,
  `internal/gitops/precommit.go:24-25`). When false, a non-zero command is
  recorded but the commit proceeds — useful for advisory linters you do not want
  to block on.

This is what makes "every Quasar commit is formatted and linted" a property of
the system rather than a hope.

## Token scopes

Scope the bot user's GitHub PAT as narrowly as the work allows:

| Repo visibility | Scope | Why |
|---|---|---|
| Public | `public_repo` | Read issues, push `quasar/*` branches, open PRs. |
| Private | `repo` | Same, for a private repository. |

**Never** grant `admin:*`, `delete_repo`, or workflow-write scopes. Quasar
neither needs nor uses them: the forge adapter only creates PRs
(`internal/forge/forge.go`), and the perimeter forbids every destructive
operation those scopes would unlock.

**Secrets are never inlined.** A literal `token:` in `.quasar.yaml` is a
config-load error; use `token_env` or `token_file` instead. An arch test in
`internal/arch_test/` fails the build if a committed config carries an inline
token, so a leaked credential cannot land via the config file.

## What to do if Quasar misbehaves

1. **Stop it.** The fleet supervisor is a foreground process — `Ctrl-C` on the
   `quasar fleet` terminal halts the supervisor loop. In-flight runs stop being
   advanced; nothing else is touched.
2. **Read the supervisor log.** The supervisor routes its diagnostics to
   `.quasar/supervisor.log` (`cmd/fleet.go:176`) — a plain text log alongside
   the fabric DB — so they never corrupt the Bubble Tea TUI on stderr.
3. **Find stuck runs.** Inspect `constellation_runs.state` in the fabric DB; a
   run wedged in `running`/`paused`/`blocked_on_review` is non-terminal. There
   is currently **no `quasar runs kill` command**. To force a stuck run
   terminal, stop the supervisor and update the row directly:

   ```sh
   sqlite3 .quasar/fabric.db \
     "UPDATE constellation_runs SET state = 'killed' WHERE id = '<run-id>';"
   ```

   Marking it terminal also makes its worktree reclaimable by the GC reaper.
4. **Recover state if needed.** A nebula the GC soft-deleted can be restored
   within its grace window with `quasar nebula undelete <id>` (see
   [gc.md](gc.md#cli-surface)).

Because every git write goes through `internal/gitops/`, the worst a runaway run
can do is push to a `quasar/*` branch and open a PR — it cannot touch your base
branch, force-push, or merge.

## Audit trail

Quasar appends structured event logs you can tail for forensics. All but the
supervisor log are JSONL (one JSON object per line); paths are relative to the
repo's `.quasar/` data directory.

| Log | Path | Format | Writer |
|---|---|---|---|
| GC audit | `.quasar/gc-audit.log` | JSONL | `internal/gc/audit.go:65` (`AuditLog.Append`); path `cmd/gc.go:115` |
| Conflict resolutions | `.quasar/telemetry/conflict_resolutions.jsonl` | JSONL | `internal/telemetry/conflict_resolutions.go:54` (`Record`); path `cmd/conflicts.go:15` |
| Coordination log | `.quasar/telemetry/coordination_log.jsonl` | JSONL | `internal/telemetry/coordination_log.go:94` (`append`); path `cmd/coordination.go:15` |
| Health events | `.quasar/telemetry/health_events.jsonl` | JSONL | `internal/telemetry/health_events.go:60` (`Record`); path `cmd/coder.go:14` |
| Cache metrics | `.quasar/telemetry/cache_metrics.jsonl` | JSONL | `internal/telemetry/cache_metrics.go:78` (`Record`); path `cmd/cache.go:14` |
| Supervisor | `.quasar/supervisor.log` | plain text | `cmd/fleet.go:176` |

What each captures:

- **GC audit** — one line per mark / sweep / reap, with the affected entity and
  byte counts. Tail it with `quasar gc audit`. See
  [gc.md](gc.md#the-audit-log).
- **Conflict resolutions** — one line per conflict-resolver invocation (rate,
  cost, latency). Summarize with `quasar conflicts report`.
- **Coordination log** — one line per cross-phase coordination check or
  override. Summarize with `quasar coordination report`.
- **Health events** — one line per coder-subprocess state transition, used to
  diagnose dead-coder terminations. Summarize with `quasar coder report`.
- **Cache metrics** — one line per invocation's prompt-cache hit/miss accounting.
  Summarize with `quasar cache report`.

The CLI summarizers for each of these are documented in [cli.md](cli.md).

## For future contributors

The wrapper is the perimeter. Keeping it airtight requires discipline:

- **Adding a method to `internal/gitops/`** is three coordinated edits: the
  validation/allowlist inside the wrapper, the arch test that protects the choke
  point (`internal/arch_test/safety_test.go`), and **this document**.
- **Migrating a grandfathered caller** off direct git: move it onto
  `internal/gitops/` and delete its entry from `gitExecExceptions`
  (`internal/arch_test/safety_test.go:30`).
- **Introducing a new `exec.Command("git"/"gh", …)`** outside the allowed
  packages will be blocked by the arch tests above. Route the call through the
  wrapper instead.
