+++
id = "gitops-and-pre-commit"
title = "Vanilla-git output safety perimeter plus per-repo pre-commit hook runner"
type = "task"
priority = 1
depends_on = ["integration-layer"]
scope = [
    "internal/gitops/gitops.go",
    "internal/gitops/gitops_test.go",
    "internal/gitops/push.go",
    "internal/gitops/push_test.go",
    "internal/gitops/commit.go",
    "internal/gitops/commit_test.go",
    "internal/gitops/precommit.go",
    "internal/gitops/precommit_test.go",
    "internal/gitops/remote.go",
    "internal/gitops/remote_test.go",
    "internal/config/config.go",
    "internal/config/config_test.go",
    "internal/arch_test/safety_test.go",
]
+++

## Problem

Two coupled concerns, both critical to making Quasar safe to run autonomously and pleasant to drop into any project:

1. **Output safety perimeter.** Quasar must never push to a base branch (`main`/`master`/etc.), force-push outside its own `quasar/*` ref namespace, delete branches it didn't create, or invoke `gh pr merge`. Today there is no central place that enforces these rules — git invocations are scattered across `cmd/`, `internal/nebula/`, and `internal/loop/`. The design spec calls for a wrapper layer (`internal/gitops/`) plus arch tests that forbid direct `exec.Command("git", …)` outside it.
2. **Per-repo pre-commit hook runner.** Each project Quasar watches has its own developer workflow conventions — `gofmt -w`, `tofu fmt`, `prettier --write`, `ruff format`, `cargo fmt`, etc. The user wants `.quasar.yaml`'s `[pre_commit]` block to declare a list of commands that run before every Quasar commit; this turns Quasar into a respectful tenant in whatever repo it touches and dramatically improves the diff quality the master reviewer eventually evaluates.

Both live naturally in `internal/gitops/` because the pre-commit runner sits inside the commit wrapper.

The user's explicit constraint is vanilla `git` — not `gh` — for all git operations. `gh` may be used for ticket reading (phase 2) and (in Nebula 3) PR creation, but never for push/branch/fetch/commit. This keeps Quasar's output side compatible with any forge.

## Solution

### Package layout

Create `internal/gitops/` with these files:
- `gitops.go` — `Client` struct, constructor, the injectable `runGit` hook, shared helpers (CWD handling, env)
- `remote.go` — `OriginURL`, `ParseRemoteURL(url) (host, owner, repo, error)`, `Fetch(ctx, ref)`
- `push.go` — `Push(ctx, branch)` with the `quasar/*` allowlist
- `commit.go` — `Commit(ctx, msg)` which runs pre-commit hooks first, then `git add -u && git commit`
- `precommit.go` — `RunPreCommit(ctx, commands, failOnError) error` with a captured-output test seam

All exported functions are methods on a `*Client` to keep test isolation easy.

### `Client` struct

```go
type Client struct {
    workDir string // worktree root
    runGit  func(ctx context.Context, args ...string) ([]byte, []byte, error) // stdout, stderr, error
    // precommit configuration is passed per-Commit call rather than stored
    // here, so a single Client serves multiple nebula worktrees if needed.
}

func New(workDir string) *Client
func NewWithRunner(workDir string, runner func(ctx context.Context, args ...string) ([]byte, []byte, error)) *Client
```

### Push safety

`Push(ctx, branch)` rules — implemented as plain function checks at the top, before any subprocess:

1. `branch` must match `^quasar/[A-Za-z0-9._/-]+$`. Non-match returns `ErrUnsafeRef` with the branch name in the message.
2. Strip any leading `refs/heads/` for normalization.
3. Refuse hard-coded forbidden patterns: `main`, `master`, `develop`, `trunk`, `production`, `prod`, `release`, `staging` — even if somehow matched by step 1 (defense in depth). Reads the list from a package-level constant `forbiddenPushBranches`; not config-overridable to prevent users from accidentally disabling the guard.
4. Subprocess: `git push origin <branch> --force-with-lease`. Force-with-lease is intentional — Quasar owns `quasar/*` branches and may rewrite history during fix cycles, but `--force-with-lease` fails safely if another agent (a human pushing manually) has updated the branch.
5. On exit code 1 with stderr matching "rejected" + "non-fast-forward", surface as a typed `ErrForcePushRejected` with a message naming the safest recovery (`quasar abandon <id>` or human intervention).

A separate exported helper, `IsQuasarBranch(name string) bool`, makes the allowlist reusable elsewhere (e.g. `internal/forge/` in Nebula 3 checks PR head refs against it).

### Other forbidden ops

The package exposes ONLY these public methods:
- `Status(ctx) (clean bool, err error)`
- `HeadSHA(ctx) (string, error)`
- `OriginURL(ctx) (string, error)`
- `CreateBranch(ctx, name string) error`  — must be `quasar/*`, refuses otherwise
- `CheckoutBranch(ctx, name string) error` — branch must exist
- `CurrentBranch(ctx) (string, error)`
- `Fetch(ctx, ref string) error`
- `Push(ctx, branch string) error`
- `Commit(ctx, message string, opts CommitOpts) (sha string, err error)`
- `AddTracked(ctx) error`  — `git add -u` (only modifications, not new files; new files come via the agent's Write tool which the loop tracks separately)
- `Diff(ctx, baseRef, headRef string) (string, error)`
- `Log(ctx, opts LogOpts) ([]CommitInfo, error)`

There is no public method that wraps `git push --force` (unconditional), `git push origin :<ref>` (delete), `git branch -D` against base branches, `git reset --hard`, `git rebase -i`, or `git checkout -- <file>`. If the loop later needs one of these, it is added with the same allowlist treatment.

### Pre-commit runner

```go
type PreCommitConfig struct {
    Commands    []string // shell-quoted command strings; each runs as `sh -c <cmd>`
    FailOnError bool     // when true, any non-zero exit aborts the commit
}

// RunPreCommit executes each command in order with workDir as CWD.
// stdout/stderr from each command is captured and returned in the result;
// the caller (the loop or the supervisor) decides whether to surface them.
//
// If FailOnError is true and any command returns non-zero, the function
// returns the first failure as ErrPreCommitFailed with the command, exit
// code, and captured stderr embedded.
func (c *Client) RunPreCommit(ctx context.Context, cfg PreCommitConfig) ([]PreCommitResult, error)
```

`sh -c <cmd>` is used so users can write composite commands like `"gofmt -w . && goimports -w ."` without Quasar parsing shell. On Windows, document the limitation and recommend `sh -c` via Git Bash; do not silently break.

### Commit wrapper

```go
type CommitOpts struct {
    PreCommit PreCommitConfig // zero value = no pre-commit step
    Author    string          // e.g. "Quasar <quasar@noreply.local>"; empty = repo default
}

func (c *Client) Commit(ctx context.Context, message string, opts CommitOpts) (string, error)
```

Sequence:
1. Run `RunPreCommit(ctx, opts.PreCommit)`; if it returns an error and `FailOnError` was true, return it.
2. Re-stage modifications introduced by pre-commit commands: `git add -u`.
3. Check the index isn't empty (`git diff --cached --quiet`). If empty AFTER pre-commit, return `ErrNothingToCommit` — this is not necessarily an error (the agent may have committed already, or the pre-commit normalized a no-op change).
4. `git commit -m <message>` (plus `--author <author>` if non-empty).
5. Return the new HEAD SHA.

### Config schema additions

Extend `internal/config/config.go` with the `[pre_commit]` block:
```yaml
[pre_commit]
commands = []                  # repo-specific
fail_on_error = true
```

Plumb the parsed `PreCommitConfig` into the existing config struct and through to the loop, where it will be passed to `gitops.Client.Commit` (loop integration is part of phase 4's wider refactor — for this phase the config is parsed but consumed only by direct gitops tests).

### Arch tests

Create `internal/arch_test/safety_test.go` containing:

```go
func TestNoDirectGitExecOutsideGitops(t *testing.T)
func TestNoDirectGHExecOutsideAllowedPackages(t *testing.T)
func TestNoForbiddenGitSubcommands(t *testing.T)
```

Implementations:
- `TestNoDirectGitExecOutsideGitops` parses every `.go` file in `cmd/` and `internal/` (excluding `internal/gitops/`, test files, and `internal/arch_test/`), walks AST looking for `exec.Command` or `exec.CommandContext` whose first arg is the string literal `"git"`. Any hit is a violation.
- `TestNoDirectGHExecOutsideAllowedPackages` does the same for `"gh"`, allowlisting `internal/integrations/github/` and (eventually) `internal/forge/`. Forge package may not exist yet at this phase — the test reads the allowlist from a constant slice so future packages are added with a single-line edit.
- `TestNoForbiddenGitSubcommands` grep-style: walks `.go` source under `internal/gitops/` for string literals that suggest a forbidden subcommand (e.g. `"push --force"` without `-with-lease`, `"branch -D main"`, `"reset --hard"`). This is a smell-detector, not airtight, but it catches the obvious cases during code review.

### What this phase does NOT do

- Loop integration of pre-commit (the loop's `runCoderPhase` will start calling `gitops.Client.Commit` with `PreCommit` populated from config — that wiring is part of phase 4's broader rework when the architect adapter changes).
- Migration of existing scattered git calls (the current `internal/loop/git.go` and `internal/nebula/git.go` callers will continue to work in-place; gradual migration is fine and the arch test starts as a `t.Skip()`-able assertion gated behind an env var so this phase can land independently of a tree-wide cleanup).

The arch test gating: by default, the test fails. A package owner who is mid-migration can set `QUASAR_ARCH_TEST_GIT_WALL=warn` to convert failures to logged warnings. The README notes this as a temporary measure that will be removed in Nebula 3 once all callers are migrated.

## Files

- `internal/gitops/gitops.go` (new) — Client, New, NewWithRunner, shared types, errors
- `internal/gitops/remote.go` (new) — OriginURL, ParseRemoteURL, Fetch
- `internal/gitops/push.go` (new) — Push, IsQuasarBranch, ErrUnsafeRef, ErrForcePushRejected, forbidden-branch constants
- `internal/gitops/commit.go` (new) — Commit, AddTracked, ErrNothingToCommit
- `internal/gitops/precommit.go` (new) — RunPreCommit, PreCommitConfig, PreCommitResult, ErrPreCommitFailed
- `internal/gitops/*_test.go` (new) — unit tests with injected runner; cover all error paths
- `internal/config/config.go` — add `PreCommit PreCommitConfig` to Config struct (or a thin DTO that maps to gitops.PreCommitConfig); parse `[pre_commit]` block; default `fail_on_error = true`
- `internal/config/config_test.go` — yaml round-trips for `[pre_commit]`, defaults, empty list
- `internal/arch_test/safety_test.go` (new) — the three arch tests above, gated by `QUASAR_ARCH_TEST_GIT_WALL` for incremental migration

## Acceptance Criteria

- [ ] `internal/gitops/` package compiles; `go test ./internal/gitops/...` passes
- [ ] `Push(ctx, "quasar/foo")` produces a `git push origin quasar/foo --force-with-lease` invocation (verifiable via the injected runner)
- [ ] `Push(ctx, "main")` returns `ErrUnsafeRef` without invoking the runner
- [ ] `Push(ctx, "feature/auth")` returns `ErrUnsafeRef`
- [ ] `Push(ctx, "quasar/main")` is allowed (matches the regex; the literal "main" check is only against unprefixed branches)
- [ ] `IsQuasarBranch("quasar/foo")` returns true; `IsQuasarBranch("main")` returns false
- [ ] `Commit` with `PreCommit.Commands = ["gofmt -w ."]` runs the command before committing
- [ ] `Commit` with a failing pre-commit command and `FailOnError = true` returns `ErrPreCommitFailed` and does NOT invoke `git commit`
- [ ] `Commit` with a failing pre-commit command and `FailOnError = false` proceeds to commit and includes the failure in the returned results slice
- [ ] `Commit` with empty index after pre-commit returns `ErrNothingToCommit`
- [ ] No public method in `internal/gitops/` wraps `git push --force` (without `--with-lease`), `git push origin :<ref>`, `git branch -D` on a base branch, `git reset --hard`, or `git rebase -i`
- [ ] `internal/arch_test/safety_test.go` exists and runs; `TestNoDirectGitExecOutsideGitops`, `TestNoDirectGHExecOutsideAllowedPackages`, and `TestNoForbiddenGitSubcommands` are present
- [ ] When `QUASAR_ARCH_TEST_GIT_WALL=warn`, arch test failures are downgraded to `t.Log` messages and the test still passes; with the env var unset or any other value, failures are real
- [ ] `[pre_commit]` block parses from `.quasar.yaml` with `commands` (string array) and `fail_on_error` (bool, default true)
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all exit 0
