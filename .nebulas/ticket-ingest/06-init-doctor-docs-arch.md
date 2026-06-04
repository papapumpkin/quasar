+++
id = "init-doctor-docs-arch"
title = "Add `quasar init`, `quasar doctor`, docs/safety.md, and final arch tests"
type = "task"
priority = 2
depends_on = ["nebula-new-from-source"]
scope = [
    "cmd/init.go",
    "cmd/init_test.go",
    "cmd/doctor.go",
    "cmd/doctor_test.go",
    "cmd/validate.go",
    "docs/safety.md",
    "docs/integrations.md",
    "CLAUDE.md",
    "README.md",
    "internal/arch_test/integrations_test.go",
    "configs/default.yaml",
]
+++

## Problem

The integration layer, GitHub adapter, gitops perimeter, and `nebula new` command are all in place. To make this nebula's deliverable usable out of the box we need three remaining pieces:

1. **`quasar init`** — scaffold a `.quasar.yaml` in the current directory with sensible defaults: auto-detect the project's language and pre-populate `[verify]`, auto-detect the GitHub remote and pre-populate `[integrations.github].repo`, leave `[pre_commit]` empty with a commented example, leave secrets as env-var references.
2. **`quasar doctor`** — diagnose configuration problems. Reports which integrations are registered vs. configured, validates secrets resolve, runs `git remote -v` and confirms the configured `repo` matches, validates the worktree is a git repo, runs `gh --version` if a GitHub integration is configured, runs pre-commit commands with `--dry-run` semantics where possible (e.g. `gofmt -l` instead of `gofmt -w`). Prints a structured report and exits non-zero if any check fails.
3. **Docs and final arch tests** — `docs/safety.md` explains the perimeter (what Quasar can/can't do, how the guardrails are enforced, what to do if you hit `ErrUnsafeRef`); `docs/integrations.md` explains the integration pattern and how to add a new adapter; CLAUDE.md and README.md get sections referencing both; a final arch test in `internal/arch_test/integrations_test.go` enforces the layering rules for the new packages.

Replace the existing `cmd/validate.go` (which has been a thin stub since Nebula 0 removed beads) with `cmd/doctor.go`. Keep `quasar validate` as a back-compat alias.

## Solution

### `quasar init`

```
Usage: quasar init [--force] [--from-template <name>]

Creates .quasar.yaml in the working directory. Refuses to overwrite an
existing file unless --force is given.

Auto-detection performed (each independently — failures are non-fatal,
the corresponding section is left as a commented placeholder):
  - Project language: scan for go.mod, package.json, Cargo.toml,
    pyproject.toml, Gemfile, mix.exs, pom.xml, build.gradle, Makefile.
    Populate [verify] commands accordingly (see CLAUDE.md table).
  - GitHub remote: git remote get-url origin parsed via gitops.ParseRemoteURL.
    If it resolves to a github.com URL, populate [integrations.github].repo.
  - Default model and prompts: copied from the built-in defaults.

[pre_commit] is added with commands = [] and fail_on_error = true plus a
commented-out example block showing common formatters (gofmt, tofu fmt,
prettier, ruff format, cargo fmt). The user uncomments the relevant lines.

Secrets are NOT written. The generated yaml documents token_env =
"GITHUB_TOKEN" and a comment pointing at the env-var or Docker-secret
pattern.
```

`init` is idempotent in the sense that running it twice on the same dir errors the second time (no merge logic — we are not building a config-management tool). With `--force` it overwrites, with confirmation prompt suppressed only if `--yes` is also passed.

### `quasar doctor`

```
Usage: quasar doctor [--json]

Output (text mode):
  ✓ git: worktree at /path/to/repo (origin: https://github.com/owner/repo)
  ✓ config: .quasar.yaml loaded
  ✓ integrations.github: configured (repo=owner/repo)
    ✓   credentials: file /run/secrets/github_token (mode 0600)
    ✓   gh CLI: gh version 2.x found
  ✓ pre_commit: 2 commands configured, all executables on PATH
  ✗ verify: `go test ./...` not configured — populate [verify].test to
            enable test gates in Nebula 3
  ✓ overall: ready

Exit code: 0 if all required checks pass, 1 otherwise. With --json, the
output is a structured JSON object suitable for CI.
```

Checks (each is a small function in `cmd/doctor.go`, listed in the order shown above):
1. Worktree: `gitops.Status()` succeeds and the result is a git repo
2. Config: `config.Load()` succeeds
3. For each `[integrations.*]` block, attempt to construct the adapter via the registry and report missing / unconfigured / unregistered cases
4. For each configured integration, resolve credentials via `ResolveSecret` and report success/failure (do NOT print the secret value)
5. For GitHub specifically, run `gh --version` (timeout 2s) and report
6. For each `[pre_commit].commands` entry, check the first token is on PATH
7. `[verify].test/.lint/.build` — non-empty triggers a green check, empty triggers a yellow warning (not a failure — verify is optional)

Each check returns `(name, status, message)` and the printer formats them. The JSON path returns the same data as an array.

### `cmd/validate.go`

Reduced to:
```go
var validateCmd = &cobra.Command{
    Use:        "validate",
    Short:      "Alias for `quasar doctor`",
    Deprecated: "use `quasar doctor` instead",
    RunE:       runDoctor,
}
```

Existing callers continue to work, and the deprecation warning appears.

### `docs/safety.md`

Sections:
1. **What Quasar can do** — push to `quasar/*` branches it owns, read tickets via configured integrations, run pre-commit commands inside the worktree, invoke `claude -p` and `gh issue view`/`gh issue list`.
2. **What Quasar cannot do** — push to `main`/`master`/`develop`/etc., force-push without lease to anything, `git branch -D` a base branch, `git reset --hard`, `gh pr merge`, `gh pr close`, `gh issue close`, `gh issue edit`, `gh repo delete`. Includes the exact regex (`^quasar/[A-Za-z0-9._/-]+$`) so readers can recognize the perimeter.
3. **How the guardrails are enforced** — three layers: wrapper packages (`internal/gitops/`), arch tests, agent prompt boundaries. Mention that the agent prompt boundary is partial in this nebula (will be wired through fully in Nebula 3 when the loop's commit path migrates to `gitops.Client.Commit`).
4. **Common errors and how to read them** — `ErrUnsafeRef`, `ErrForcePushRejected`, `ErrPreCommitFailed`, `ErrNothingToCommit`.
5. **For future contributors** — adding a method to `internal/gitops/` requires updating the allowlist, the arch test, AND `docs/safety.md`. PRs that touch git/gh outside the wrappers will be blocked by CI.

### `docs/integrations.md`

Sections:
1. **The integration pattern** — TicketSource and Forge interfaces; registry; secret resolver.
2. **Adding a new ticket source** — concrete walkthrough using a hypothetical Jira adapter. Shows: package layout, init() registration, parsing a source-id, Fetch implementation, credential handling.
3. **`.quasar.yaml` configuration** — `[integrations.<name>]` block conventions, `token_env`, `token_file`, never-inline-token rule.
4. **Testing integrations** — how to write tests without hitting the real service (parse functions + injected exec hook).

### CLAUDE.md updates

Add a new "Integrations & Safety" section that:
- Names the two interfaces and where they live
- References `docs/safety.md` for the perimeter
- References `docs/integrations.md` for the add-an-adapter walkthrough
- Adds `[pre_commit]` to the build/test commands list with a one-line description
- Updates the "Project Structure" tree to include `internal/integrations/`, `internal/gitops/`, `internal/forge/` (forge as a reserved stub)

### README.md updates

A new "External Trackers" subsection under "What It Does," explaining that Quasar can ingest work from GitHub Issues today, with the integration pattern open to Jira/Linear/etc. tomorrow. Cross-reference `docs/integrations.md`. Update the Prerequisites bullets: `gh` is now optional unless `[integrations.github]` is configured.

### Final arch tests

`internal/arch_test/integrations_test.go`:
- `TestIntegrationsLayering` — no package outside `internal/integrations/github/` imports `internal/integrations/github` (only the cmd layer can, transitively, through the registry init pattern); the cmd layer may import `internal/integrations` but not its subpackages directly except for blank-import side effects.
- `TestNoInlineTokens` — scan `configs/` and any committed `.quasar.yaml` for a `token: ` line; fail if found.
- `TestForgeStubMinimal` — assert `Forge` interface has exactly one method (`Name`); if a future PR adds methods here without doing the proper rollout in Nebula 3, this test catches it.

### configs/default.yaml updates

Add commented-out template blocks for `[integrations.github]`, `[forge.github]`, `[pre_commit]`, and `[verify]`. The shipped default is empty (no integrations) so a freshly-installed Quasar that runs `quasar init` produces the right config.

## Files

- `cmd/init.go` (new) — runInit with auto-detection + `[pre_commit]` placeholder generator
- `cmd/init_test.go` (new) — TempDir-based tests for each detection branch + no-overwrite + --force
- `cmd/doctor.go` (new) — runDoctor with the checks above; text and JSON output modes
- `cmd/doctor_test.go` (new) — fakes for each check, exit-code assertions
- `cmd/validate.go` — reduce to a deprecated alias of `runDoctor`
- `docs/safety.md` (new) — perimeter doc per the outline above
- `docs/integrations.md` (new) — integration pattern doc
- `CLAUDE.md` — add Integrations & Safety section; update Project Structure; mention `[pre_commit]`
- `README.md` — add External Trackers subsection; update Prerequisites for the optional `gh`
- `internal/arch_test/integrations_test.go` (new) — three arch tests above
- `configs/default.yaml` — commented templates for `[integrations.*]`, `[forge.*]`, `[pre_commit]`, `[verify]`

## Acceptance Criteria

- [ ] `quasar init` in an empty directory creates `.quasar.yaml` with the documented sections present
- [ ] `quasar init` in a directory containing `go.mod` populates `[verify]` with `go test ./...`, `go vet ./...`, `go build ./...`
- [ ] `quasar init` in a directory with an https GitHub remote populates `[integrations.github].repo` correctly
- [ ] `quasar init` refuses to overwrite an existing `.quasar.yaml` without `--force`
- [ ] `quasar doctor` exits 0 when all configured integrations are reachable, credentials resolve, and the worktree is a git repo
- [ ] `quasar doctor` exits 1 when credential resolution fails for any configured integration
- [ ] `quasar doctor --json` produces parseable JSON; the schema is stable enough for CI checks
- [ ] `quasar validate` still works and prints a deprecation notice pointing at `quasar doctor`
- [ ] `docs/safety.md` exists and documents the regex, forbidden branches, allowlist, and the three enforcement layers
- [ ] `docs/integrations.md` exists and walks through adding a hypothetical Jira adapter
- [ ] `CLAUDE.md` references both docs and the new package locations
- [ ] `README.md` mentions external trackers and the new optional `gh` dependency
- [ ] `internal/arch_test/integrations_test.go` runs and passes; layering rules are enforced
- [ ] `TestNoInlineTokens` scans committed configs and finds no `token: ` inline values
- [ ] `TestForgeStubMinimal` passes (Forge has exactly one method); the test self-documents how to expand it in Nebula 3
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all exit 0
- [ ] A user starting from a fresh clone can run `git clone … && cd … && quasar init && $EDITOR .quasar.yaml && quasar doctor && quasar nebula new github:<n>` and produce a draft nebula end-to-end
