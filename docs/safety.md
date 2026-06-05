# Quasar Output Safety Perimeter

Quasar runs autonomous agents that write code and (in a later release) push
branches and open pull requests. This document describes the **output safety
perimeter**: the boundary that constrains what Quasar can do to your repository
and your forge, and how that boundary is enforced.

> **Status note.** The vanilla-git wrapper package (`internal/gitops/`) that
> centralizes git *write* operations, and the agent commit path that routes
> through it, land in a later nebula (the master review/PR loop). This document
> describes the perimeter as the project's safety architecture. Where a layer is
> only partially wired today, that is called out explicitly.

## What Quasar can do

- **Push to `quasar/*` branches it owns.** Every branch Quasar creates is
  namespaced under `quasar/`. The push wrapper refuses any other target ref.
- **Read tickets** via configured integrations (today: GitHub Issues through the
  `gh` CLI — `gh issue view`, `gh issue list`). Reads only.
- **Run pre-commit commands** inside the worktree (formatters/linters you
  configure under `[pre_commit]`).
- **Invoke `claude -p`** for the coder and reviewer agents.

## What Quasar cannot do

The perimeter forbids every destructive or out-of-namespace operation:

- Push to a base branch — `main`, `master`, `develop`, `release/*`, etc.
- Force-push without lease to anything.
- `git branch -D` a base branch.
- `git reset --hard`.
- `gh pr merge`, `gh pr close`, `gh issue close`, `gh issue edit`,
  `gh repo delete`.

The allowed-ref namespace is exactly:

```
^quasar/[A-Za-z0-9._/-]+$
```

A push whose target ref does not match this pattern is rejected at runtime with
`ErrUnsafeRef`. There is **no override flag** — the refusal is a deliberate, hard
boundary, not a panic and not something a CLI option can bypass.

## How the guardrails are enforced

Three independent layers, defense-in-depth:

1. **Wrapper packages.** All git *write* operations go through
   `internal/gitops/` (vanilla `git` via `os/exec`). It validates the target ref
   against the `quasar/*` regex before any push and rejects destructive ops
   against base branches. The `gh` CLI is confined to
   `internal/integrations/github/` and is used for *ticket reading only* — never
   for git operations, because shelling `gh` for git work would fork Quasar's
   forge support.
2. **Architecture tests.** Tests in `internal/arch_test/` assert the layering
   that keeps the wrappers the single choke point — for example, that no package
   outside the GitHub adapter imports it directly (it is reached only through the
   `integrations` registry), that committed configs contain no inline tokens
   (`TestNoInlineTokens`), and that the reserved `Forge` interface stays minimal
   (`TestForgeStubMinimal`). The arch test forbidding direct
   `exec.Command("git", …)` / `exec.Command("gh", …)` outside the wrapper
   packages lands with `internal/gitops/`.
3. **Agent prompt boundaries.** The coder/reviewer prompts describe the
   perimeter so agents do not attempt forbidden operations. **This layer is
   partial in the current release** and is wired through fully when the loop's
   commit path migrates to the gitops wrapper in the master review/PR nebula.

No single layer is trusted alone: even if a prompt boundary is bypassed, the
wrapper refuses the operation; even if someone adds a raw `exec.Command("git",
…)`, the arch test fails CI.

## Common errors and how to read them

| Error | Meaning | What to do |
|-------|---------|------------|
| `ErrUnsafeRef` | A push targeted a ref outside `quasar/*`. | Re-target the push to a `quasar/<name>` branch. This is working as intended; there is no override. |
| `ErrForcePushRejected` | A force-push without lease was attempted. | Use a lease-based push, or push to a fresh `quasar/*` branch. |
| `ErrPreCommitFailed` | A `[pre_commit]` command exited non-zero. | Run the command yourself to see its output; fix the formatting/lint issue it reported. |
| `ErrNothingToCommit` | A commit was requested with no staged changes. | Usually benign — the agent produced no diff this cycle. |

## Sandboxing model

Each `constellation_run` executes in a **fresh git worktree** under
`.git/worktrees/quasar/<run-id>/`. The runtime never reuses a worktree across
runs, so one run can never see another's uncommitted state. Pre-commit hooks run
inside that worktree, against exactly the changes the run produced. When the run
completes (or is garbage-collected), its worktree directory is removed.

## Token scopes

The bot user's GitHub PAT should be scoped as narrowly as the work allows:

- `public_repo` (public repos) or `repo` (private repos) — enough to read issues
  and open PRs to `quasar/*` branches.
- **No** `admin:*`, **no** `delete_repo`, **no** workflow-write scopes.

The `gh_open_pr` builtin uses this token for PR creation only; it is never used
for destructive forge operations.

## What to do if Quasar misbehaves

1. **Stop it.** Kill the supervisor (`quasar kill`, or `systemctl stop quasar`).
   Multi-repo control is via CLI only — there is no hidden control channel.
2. **Recover state.** Recently completed nebulas can be soft-undeleted within the
   GC grace window before their blobs are swept.
3. **Investigate.** Read `gc-audit.log` (JSONL) for what was reclaimed and the
   `constellation_runs` table for what each run attempted. Because every git
   write goes through `internal/gitops/`, the worst a runaway run can do is push
   to a `quasar/*` branch — it cannot touch your base branch.

## For future contributors

The wrappers are the perimeter. Keeping them airtight requires discipline:

- **Adding a method to `internal/gitops/`** requires three coordinated edits:
  1. the allowlist / validation inside the wrapper,
  2. the architecture test that enforces the choke point, and
  3. **this document**.
- **Expanding the `Forge` interface** (PR creation, comment polling, status
  sync) is reserved for the master review/PR nebula. `TestForgeStubMinimal`
  fails the moment a method is added so the rollout cannot be done piecemeal —
  update the test's expected method count, implement the adapter side, and
  document the new capability here.
- PRs that introduce a direct `exec.Command("git", …)` or `exec.Command("gh",
  …)` outside the wrapper packages will be blocked by CI once the gitops arch
  test lands. Route the call through the wrapper instead.
