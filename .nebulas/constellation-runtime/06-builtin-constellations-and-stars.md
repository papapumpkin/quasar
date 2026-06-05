+++
id = "builtin-constellations-and-stars"
title = "Ship the default constellations/stars/skills as //go:embed defaults; architect prompt renders [pre_commit]"
type = "task"
priority = 2
depends_on = ["constellation-runtime"]
scope = [
    "internal/artifacts/defaults/constellations/**",
    "internal/artifacts/defaults/stars/**",
    "internal/artifacts/defaults/skills/**",
    "internal/artifacts/defaults/sensors/**",
    "internal/artifacts/builtins.go",
    "internal/artifacts/builtins_test.go",
    "internal/constellations/operators/render_seed_prompt.go",
    "internal/constellations/operators/render_seed_prompt_test.go",
]
+++

## Problem

With the runtime in place (Phase 5), Quasar still has no work to do at startup — there are no built-in stars or constellations. This phase ships the default set: enough constellations and stars to handle the entire ticket-to-PR happy path out of the box, all embedded into the binary via `//go:embed` so a user with zero config files installed still gets a working Quasar.

The defaults are also the canonical examples of how to author each artifact type. A user who wants to fork the default coder-reviewer for their team copies it to `<repo>/constellations/coder-reviewer.toml` and edits — same name, per-repo override wins.

Plus one targeted enhancement: the architect star's prompt renders the active `[pre_commit]` commands so generated phases anticipate the quality bar they'll be measured against.

## Solution

### Files shipped via `//go:embed`

Directory layout under `internal/artifacts/defaults/`:

```
constellations/
  architect.toml
  architect-fix.toml
  coder-reviewer.toml
  master-review.toml
  open-pr.toml
  nebula-lifecycle.toml
stars/
  architect-star.md
  coder.md
  reviewer.md
  master-reviewer-star.md
skills/
  git-aware.md
  prompt-cache-aware.md
sensors/
  README.md          (explanatory only; sensors have no embedded defaults)
```

### `constellations/coder-reviewer.toml`

```toml
name        = "coder-reviewer"
description = "The canonical implement-then-review loop. Runs per nebula phase."

[[nodes]]
id     = "implement"
type   = "star"
star   = "coder"
inputs = { phase_body = "${nebula.current_phase.body}", phase_title = "${nebula.current_phase.title}" }

[[nodes]]
id     = "review"
type   = "star"
star   = "reviewer"
inputs = { diff = "${nodes.implement.diff}", phase_body = "${nebula.current_phase.body}" }

[[edges]]
from = "implement"
to   = "review"
when = "implement.committed"

[[edges]]
from = "review"
to   = "implement"
when = "review.findings_count > 0 && cycle < nebula.execution.max_review_cycles"

[[edges]]
from = "review"
to   = "_done"
when = "review.approved"

[[edges]]
from = "review"
to   = "_failed"
when = "cycle >= nebula.execution.max_review_cycles"
```

### `constellations/architect.toml`

```toml
name        = "architect"
description = "Refines a seed nebula into executable phases. Triggered by sensors after user approval."

[[nodes]]
id   = "render_seed"
type = "builtin"
op   = "render_seed_prompt"

[[nodes]]
id     = "plan"
type   = "star"
star   = "architect-star"
inputs = { prompt = "${nodes.render_seed.output}" }

[[nodes]]
id   = "persist"
type = "builtin"
op   = "persist_phases"
inputs = { architect_output = "${nodes.plan.output}" }

[[edges]]
from = "render_seed"
to   = "plan"

[[edges]]
from = "plan"
to   = "persist"

[[edges]]
from = "persist"
to   = "_done"
```

### `constellations/master-review.toml`

```toml
name        = "master-review"
description = "Reviews a completed nebula's full diff and decides ship vs spawn-fix vs escalate."

[[nodes]]
id     = "review"
type   = "star"
star   = "master-reviewer-star"
inputs = { nebula_diff = "${nebula.full_diff}", verify_results = "${nebula.verify_results}" }

[[edges]]
from = "review"
to   = "_done"
when = "review.decision == 'ship'"

[[edges]]
from = "review"
to   = "_awaiting_human"
when = "review.decision == 'escalate'"

[[edges]]
from = "review"
to   = "_done"
when = "review.decision == 'fix' && cycle < 3"

[[edges]]
from = "review"
to   = "_awaiting_human"
when = "cycle >= 3"
```

(The decision-driven dispatch of what to do after master-review is handled by `nebula-lifecycle` — see below — not by master-review itself.)

### `constellations/architect-fix.toml`

Identical shape to `architect.toml` but reads `nebula.master_review.fix_feedback` instead of the original source context. Produces additional phases appended to the nebula. The phase iterator picks them up automatically.

### `constellations/open-pr.toml`

```toml
name        = "open-pr"
description = "Pushes the nebula's branch and opens a PR. Terminal step in the happy path."

[[nodes]]
id   = "push"
type = "builtin"
op   = "gitops_push"
inputs = { branch = "${nebula.branch}" }

[[nodes]]
id   = "create_pr"
type = "builtin"
op   = "gh_open_pr"
inputs = {
  base = "${nebula.execution.base_branch}",
  head = "${nebula.branch}",
  title = "${nebula.name}",
  body = "${nebula.pr_body}",
}

[[edges]]
from = "push"
to   = "create_pr"

[[edges]]
from = "create_pr"
to   = "_done"
```

### `constellations/nebula-lifecycle.toml`

```toml
name        = "nebula-lifecycle"
description = "Standard lifecycle: per-phase execution, then master review, then ship or fix."

[[nodes]]
id   = "execute_phases"
type = "phase_iterator"
inputs = { sub_constellation = "coder-reviewer", parallel = "${nebula.execution.max_workers}" }

[[nodes]]
id   = "master_review"
type = "constellation"
ref  = "master-review"

[[nodes]]
id   = "ship"
type = "constellation"
ref  = "open-pr"

[[nodes]]
id   = "fix"
type = "constellation"
ref  = "architect-fix"

[[edges]]
from = "execute_phases"
to   = "master_review"
when = "execute_phases.all_phases_complete"

[[edges]]
from = "master_review"
to   = "ship"
when = "master_review.decision == 'ship'"

[[edges]]
from = "master_review"
to   = "fix"
when = "master_review.decision == 'fix' && fix_cycles < 3"

[[edges]]
from = "master_review"
to   = "_awaiting_human"
when = "master_review.decision == 'escalate' || fix_cycles >= 3"

[[edges]]
from = "fix"
to   = "execute_phases"
when = "fix.phases_appended"

[[edges]]
from = "ship"
to   = "_done"
when = "ship.pr_opened"
```

### `stars/coder.md`

```markdown
+++
name = "coder"
model = "claude-sonnet-4-6"
fallback_model = "claude-haiku-4-5"
skills = ["git-aware", "prompt-cache-aware"]

[tools]
allowed = ["Read", "Edit", "Write", "Glob", "Grep", "Bash(go *)", "Bash(git diff *)", "Bash(git status)"]
denied  = ["Bash(git push *)", "Bash(gh pr merge *)", "Bash(git reset *)"]

[defaults]
max_budget_usd = 0.50
effort = "medium"
+++

You are the coder. Implement the task described to you as a single focused
change. Read existing code first to understand the codebase's conventions.
Stay within the scope of the task — no drive-by refactors.

When done, commit your changes. The repository's pre-commit hooks will run
formatters, linters, builds, and tests before the commit lands; if any
fail, address them and commit again. Do not bypass the checks.

Use imperative-mood commit messages under 72 chars.
```

### `stars/reviewer.md`

```markdown
+++
name = "reviewer"
model = "claude-sonnet-4-6"
fallback_model = "claude-haiku-4-5"
skills = ["git-aware"]

[tools]
allowed = ["Read", "Glob", "Grep", "Bash(git diff *)", "Bash(git log *)"]
denied  = ["Edit", "Write", "Bash(git push *)", "Bash(gh pr merge *)"]

[defaults]
max_budget_usd = 0.30
effort = "medium"
+++

You are the reviewer. Inspect the coder's diff and judge whether it solves
the stated task correctly and idiomatically. Use git diff to see the
changes; use Read/Glob/Grep to inspect surrounding code for context.

End your review with one of:
  APPROVED: <one-line ship rationale>
  ISSUES:
    1. <severity> | <description>
    2. ...

Severity is one of: critical, major, minor.
```

### `stars/architect-star.md`

```markdown
+++
name = "architect-star"
model = "claude-sonnet-4-6"
fallback_model = "claude-haiku-4-5"
skills = ["git-aware"]

[tools]
allowed = ["Read", "Glob", "Grep", "Bash(git log *)", "Bash(git diff *)"]
denied  = ["Edit", "Write", "Bash(git push *)", "Bash(git commit *)"]

[defaults]
max_budget_usd = 1.00
effort = "high"
+++

You are the architect. Given a seed nebula (a unit of work pulled from
an external tracker), explore the codebase to understand what's there,
then produce a structured multi-phase plan for executing the work.

Output a TOML document with [[phase]] blocks. Each phase has:
  id       — short kebab-case identifier
  title    — concise human-readable summary
  body     — Markdown describing the work, including files to touch,
             approach, and acceptance criteria
  type     — "task", "feature", or "bug"
  priority — integer (1 is highest)
  depends_on — array of phase IDs this phase depends on

Plan phases small enough to validate independently. Each phase should be
self-contained: a coder agent reads its body and implements it without
needing to look at other phases.

This repository enforces the following pre-commit checks. Generated phases
must produce code that passes all of them:
${pre_commit_commands}

Plan accordingly: include test additions in phases that change behavior;
include build validation if the language requires it; include lint
adjustments if you anticipate format changes.
```

The `${pre_commit_commands}` placeholder is interpolated by the `render_seed_prompt` builtin operator at runtime — read from the repo's `.quasar.yaml` `[pre_commit]` block, rendered as a bulleted list.

### `stars/master-reviewer-star.md`

```markdown
+++
name = "master-reviewer-star"
model = "claude-sonnet-4-6"
fallback_model = "claude-haiku-4-5"

[tools]
allowed = ["Read", "Glob", "Grep", "Bash(git diff *)", "Bash(git log *)"]
denied  = ["Edit", "Write", "Bash(git push *)", "Bash(gh pr merge *)"]

[defaults]
max_budget_usd = 1.50
effort = "high"
+++

You are the master reviewer. A nebula has completed all its phases — the
coder-reviewer loop has approved each phase individually. Your job is to
review the cumulative diff and make one of three decisions:

  ship      — the work is ready to PR. Provide a one-paragraph rationale.
  fix       — the work is mostly right but needs targeted changes. Provide
              specific feedback the architect can use to plan fix phases.
  escalate  — there's something here that a human needs to weigh in on.
              Provide a clear summary of the concern.

Inspect the full diff. Run the verify commands (test/lint/build) the
runtime captured for you. Consider whether the change set as a whole
makes sense, not just whether each piece is locally correct.

End with one of:
  SHIP: <rationale>
  FIX: <feedback for architect>
  ESCALATE: <concern>
```

### `skills/git-aware.md`

```markdown
+++
name = "git-aware"
tools_add = [
  "Bash(git diff *)",
  "Bash(git log *)",
  "Bash(git status)",
  "Bash(git add *)",
  "Bash(git commit *)",
  "Bash(git ls-files *)",
]
+++

You have git access. Inspect existing code with `git ls-files`, check
diffs with `git diff`, commit changes with clear, imperative-mood messages
under 72 chars. Prefer small commits over large ones — each commit should
answer one question.

The repository's pre-commit hooks run automatically when you commit.
If a hook modifies files (e.g. a formatter), those changes are staged
into your commit. If a hook fails, your commit aborts — address the
failure and commit again.
```

### `skills/prompt-cache-aware.md`

```markdown
+++
name = "prompt-cache-aware"
tools_add = []
+++

Your system prompt has a stable cache prefix. When you respond, do not
re-read files you read in a previous turn unless the file has changed
or you need fresh content — re-reads add to the cost without benefit.

Prefer one tool call per file you actually need to read or modify. Plan
your reads at the start of your turn rather than interleaving them.
```

### Architect prompt enhancement

Update `internal/constellations/operators/render_seed_prompt.go`:

```go
// render_seed_prompt builds the architect's user prompt by rendering the
// seed nebula's source context plus the repo's pre-commit commands.
// The pre-commit list goes into the architect's prompt so generated
// phases anticipate the quality bar.
func renderSeedPrompt(ctx context.Context, rt *Runtime, state *State, args map[string]any) (map[string]any, error) {
    // ... existing render logic (nebula name, description, source URL,
    // goals, constraints, comments) ...

    // Append the pre-commit commands from the repo config.
    pre := rt.repoCfg.PreCommit
    if len(pre.Commands) > 0 {
        var b strings.Builder
        b.WriteString("\n\n## Repository Pre-Commit Checks\n\n")
        b.WriteString("This repository enforces the following checks before every commit. ")
        b.WriteString("Your plan must produce code that passes all of them:\n\n")
        for _, c := range pre.Commands {
            fmt.Fprintf(&b, "- `%s`\n", c)
        }
        prompt += b.String()
    }

    return map[string]any{"output": prompt}, nil
}
```

The architect star's template uses `${pre_commit_commands}`; the operator interpolates it via simple string replacement. No new abstraction.

## Files

- `internal/artifacts/defaults/constellations/architect.toml` (new)
- `internal/artifacts/defaults/constellations/architect-fix.toml` (new)
- `internal/artifacts/defaults/constellations/coder-reviewer.toml` (new)
- `internal/artifacts/defaults/constellations/master-review.toml` (new)
- `internal/artifacts/defaults/constellations/open-pr.toml` (new)
- `internal/artifacts/defaults/constellations/nebula-lifecycle.toml` (new)
- `internal/artifacts/defaults/stars/coder.md` (new)
- `internal/artifacts/defaults/stars/reviewer.md` (new)
- `internal/artifacts/defaults/stars/architect-star.md` (new)
- `internal/artifacts/defaults/stars/master-reviewer-star.md` (new)
- `internal/artifacts/defaults/skills/git-aware.md` (new)
- `internal/artifacts/defaults/skills/prompt-cache-aware.md` (new)
- `internal/artifacts/defaults/sensors/README.md` (new) — explains that sensors have no defaults
- `internal/artifacts/builtins.go` — wire `//go:embed defaults/**` to expose them via the loader
- `internal/artifacts/builtins_test.go` — assert every embedded file parses successfully through the loader
- `internal/constellations/operators/render_seed_prompt.go` — interpolate `${pre_commit_commands}` from `runtime.repoCfg.PreCommit`
- `internal/constellations/operators/render_seed_prompt_test.go` — test the pre-commit interpolation path

## Acceptance Criteria

- [ ] Every TOML file under `internal/artifacts/defaults/constellations/` parses without error via the file loader from Phase 2
- [ ] Every Markdown file under `internal/artifacts/defaults/stars/` and `skills/` parses without error
- [ ] `Loader.LoadConstellation("coder-reviewer")` returns a populated Constellation when no per-repo override exists
- [ ] `Loader.LoadStar("coder")` returns a Star whose Prompt includes both the body text and the resolved `git-aware` and `prompt-cache-aware` skill fragments; whose Tools.Allowed includes both the base allowed list AND each skill's tools_add
- [ ] The architect star's prompt template contains the literal `${pre_commit_commands}` placeholder
- [ ] When `render_seed_prompt` runs for a repo with a configured `[pre_commit]`, the placeholder is replaced with a bulleted list of the configured commands
- [ ] When `render_seed_prompt` runs for a repo without a configured `[pre_commit]`, the placeholder produces no extra block (the "## Repository Pre-Commit Checks" section is omitted)
- [ ] `nebula-lifecycle.toml` validates: edges form a connected DAG with `_done`, `_awaiting_human` as terminal nodes; no cycles excluding terminals
- [ ] A user can copy any default file into `<repo>/constellations/` (or `stars/`, `skills/`) and the per-repo override wins on next load
- [ ] `quasar lint` passes against the embedded defaults
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all exit 0
