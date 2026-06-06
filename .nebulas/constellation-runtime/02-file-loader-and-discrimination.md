+++
id = "file-loader-and-discrimination"
title = "Directory-based file loader: stars (md+frontmatter), skills (md+frontmatter), constellations (toml), sensors (toml); expression mini-language; quasar lint"
type = "task"
priority = 1
depends_on = ["multi-repo-foundation", "rename-integrations-to-sensors"]
scope = [
    "internal/artifacts/loader.go",
    "internal/artifacts/loader_test.go",
    "internal/artifacts/markdown.go",
    "internal/artifacts/markdown_test.go",
    "internal/artifacts/expr.go",
    "internal/artifacts/expr_test.go",
    "internal/artifacts/types.go",
    "internal/artifacts/types_test.go",
    "internal/artifacts/builtins.go",
    "internal/artifacts/builtins_test.go",
    "cmd/lint.go",
    "cmd/lint_test.go",
    "internal/arch_test/artifacts_test.go",
]
+++

## Problem

The pluggable substrate needs four file formats loaded uniformly: stars and skills as Markdown with TOML frontmatter (mirroring Claude Code's SKILL.md pattern), constellations and sensor-instances as plain TOML. Each artifact type has its own schema, but they share a discovery mechanism: directory location + filename. Built-in defaults ship via `//go:embed`; per-repo overrides (path resolved by Phase 0's `repos.Resolver`) win when present.

No K8s-style envelope (`kind: …`). Directory location is the discriminator. The loader knows it's a star because the file lives in `stars/`; it knows it's a constellation because the file lives in `constellations/`. Simple, predictable, no metadata-block boilerplate.

Plus a small expression evaluator: constellation TOML's `when:` strings (`review.findings_count > 0 && cycle < input.max_review_cycles`) need parsing and evaluation against a runtime state map. The grammar is deliberately tiny — see the expr.go shape below.

Plus `quasar lint`: walk all artifact directories, validate against kind-aware schemas, report errors with file:line:col. Required because authoring TOML/Markdown without a lint step is a recipe for broken constellations.

## Solution

### Package layout

Create `internal/artifacts/` with:
- `types.go` — `Star`, `Skill`, `Constellation`, `SensorInstance` struct definitions + frontmatter parsers
- `loader.go` — `Loader` struct: discovers files in per-repo dirs + embedded FS, dispatches to the right parser by directory name
- `markdown.go` — TOML-frontmatter extractor (reuses nebula-phase parser pattern: `+++` delimiters, body is everything after the closing `+++`)
- `expr.go` — expression lexer + parser + evaluator for `when:` strings
- `builtins.go` — `//go:embed defaults/**` with the in-binary defaults; resolver of the `:embedded:` sentinel from Phase 0

### Star file shape (Markdown + TOML frontmatter)

```markdown
+++
name = "coder"
model = "claude-sonnet-4-6"
fallback_model = "claude-haiku-4-5"
skills = ["git-aware", "prompt-cache-aware"]

[tools]
allowed = ["Read", "Edit", "Write", "Glob", "Grep", "Bash(go *)", "Bash(git diff *)"]
denied  = ["Bash(git push *)", "Bash(gh pr merge *)"]

[defaults]
max_budget_usd = 0.50
effort = "medium"
+++

You are the coder. Implement the task described to you as a single focused
change. Read existing code first to understand the codebase's conventions,
then make the necessary changes. Commit your changes with a clear,
imperative-mood message under 72 chars.
```

Loader produces:

```go
type Star struct {
    Name          string
    Model         string
    FallbackModel string
    Skills        []string         // resolved at load time: skill names → SkillRef
    Tools         StarTools
    Defaults      StarDefaults
    Prompt        string           // the Markdown body
    SourcePath    string           // for error reporting
}

type StarTools struct {
    Allowed []string
    Denied  []string
}

type StarDefaults struct {
    MaxBudgetUSD float64
    Effort       string
}
```

Skill resolution at load time: `Star.Skills = ["git-aware", …]` → loader resolves each name via the resolver, parses the Skill, appends its prompt fragment to the star's prompt, and unions its tools_add into Tools.Allowed.

### Skill file shape

```markdown
+++
name = "git-aware"
tools_add = [
  "Bash(git diff *)",
  "Bash(git log *)",
  "Bash(git status)",
  "Bash(git add *)",
  "Bash(git commit *)",
]
+++

You have git access. Inspect existing code with `git ls-files`, check diffs
with `git diff`, commit changes with clear, imperative-mood messages under
72 chars. Prefer small commits over large ones — each commit should answer
one question.
```

Loader produces:

```go
type Skill struct {
    Name           string
    ToolsAdd       []string
    PromptFragment string  // the Markdown body
    SourcePath     string
}
```

### Constellation file shape (TOML)

```toml
name        = "coder-reviewer"
description = "Implement-then-review loop. The canonical default."

[[nodes]]
id     = "implement"
star   = "coder"
inputs = { task = "${nebula.context.goals[0]}" }

[[nodes]]
id     = "review"
star   = "reviewer"
inputs = { diff = "${nodes.implement.diff}" }

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

[outputs]
approved   = "${nodes.review.approved}"
final_diff = "${nodes.implement.diff}"
```

Loader produces:

```go
type Constellation struct {
    Name        string
    Description string
    Nodes       []ConstellationNode
    Edges       []ConstellationEdge
    Outputs     map[string]Expression   // pre-compiled expressions
    SourcePath  string
}

type ConstellationNode struct {
    ID       string
    Type     NodeType  // "star" | "constellation" | "phase_iterator" | "builtin"
    Star     string    // populated when Type == "star"
    Ref      string    // populated when Type == "constellation" (sub-constellation name)
    Op       string    // populated when Type == "builtin"
    Inputs   map[string]Expression  // pre-compiled
}

type ConstellationEdge struct {
    From string
    To   string  // node ID, or reserved: "_done", "_failed", "_awaiting_human", "_paused"
    When Expression  // pre-compiled; nil means unconditional
}
```

`Expression` is the AST produced by the expression mini-language (see expr.go below). Expressions are parsed once at constellation load time, not on every evaluation, so the runtime is fast.

### Sensor instance file shape (TOML)

```toml
name = "github-prod-issues"
type = "github_issues"
poll_interval = "5m"

[config]
repo       = "papapumpkin/quasar"
token_env  = "GITHUB_TOKEN"
labels     = ["needs-quasar"]

[[triggers]]
constellation = "architect"
when          = "new_item"
```

Loader produces:

```go
type SensorInstance struct {
    Name         string
    Type         string                  // references a Go-registered sensor type
    PollInterval time.Duration
    Config       map[string]any          // opaque; passed to sensor.Configure
    Triggers     []SensorTrigger
    SourcePath   string
}

type SensorTrigger struct {
    Constellation string
    When          string  // event name match; simple equality for v1
}
```

### Loader API

```go
// Loader discovers, parses, and resolves artifact files for a given repo.
// Per-repo overrides take precedence over embedded defaults. Skill names
// referenced from stars are resolved transitively at load time.
type Loader struct {
    resolver *repos.Resolver
    builtins fs.FS  // //go:embed defaults/**
}

func New(r *repos.Resolver) *Loader

func (l *Loader) LoadStar(name string) (*Star, error)
func (l *Loader) LoadSkill(name string) (*Skill, error)
func (l *Loader) LoadConstellation(name string) (*Constellation, error)
func (l *Loader) LoadSensorInstance(name string) (*SensorInstance, error)

// LoadAllSensorInstances loads every <repo>/sensors/*.toml. Used by the
// supervisor at startup to know what schedulers to spin up.
func (l *Loader) LoadAllSensorInstances() ([]*SensorInstance, error)
```

All errors include `file:line:col` from the source TOML/Markdown so users can locate the problem precisely.

### Expression mini-language

`internal/artifacts/expr.go`:

```go
// Expression is a pre-compiled expression that can be evaluated against
// a runtime State.
type Expression interface {
    Eval(state State) (any, error)
    String() string
}

// State is the runtime evaluation context for expressions. It uses
// dot-notation lookup: state.Get("nodes.review.approved") returns the value
// or false (zero) if any segment is missing. Missing segments produce a
// nil result, not an error, so expressions like `nodes.review.approved`
// safely return false before review has run.
type State map[string]any

// Parse compiles an expression string into an Expression AST.
func Parse(source string) (Expression, error)
```

Supported grammar:
- Literals: `true`, `false`, integers, floats, strings (`"..."`)
- Dot access: `nodes.review.approved`, `input.max_review_cycles`, `nebula.execution.max_review_cycles`
- Comparisons: `==`, `!=`, `<`, `<=`, `>`, `>=`
- Boolean: `&&`, `||`, `!`
- Arithmetic: `+`, `-`, `*`, `/`
- Ternary: `a ? b : c`
- String interpolation: `${...}` within string contexts
- Tiny stdlib: `len(x)`, `has(map, key)`, `empty(x)`
- NO function calls beyond the stdlib
- NO arbitrary code

Parser is hand-written recursive descent with Pratt-style operator precedence. ~250 LOC of Go. Error reporting includes the source position where parsing failed.

### `quasar lint`

```
quasar lint [--repo <path>] [--strict] [--json]
```

Walks the per-repo and embedded artifact directories. Loads each file. Reports:
- Schema errors (missing required fields, unknown fields when strict mode)
- Expression parse errors with line/col
- Constellation cycle detection (in the DAG)
- Edges referencing unknown nodes
- Inline-token guardrail violations
- Star references to unknown skills
- Sensor instances with unknown types
- Constellation nodes referencing unknown stars / sub-constellations

Exit code: 0 on no errors, 1 on errors. `--strict` upgrades warnings to errors.

CI integration: `quasar lint --strict --json` produces machine-readable output suitable for a CI failure check.

### Arch tests

`internal/arch_test/artifacts_test.go`:
- `TestExpressionLanguageMinimal` — expr.go's Parse rejects any token outside the documented grammar
- `TestNoFunctionCallsBeyondStdlib` — the tiny stdlib is the only function-call surface
- `TestSchemaStrictness` — unknown TOML keys in --strict mode produce errors

## Files

- `internal/artifacts/loader.go` (new) — Loader struct + four LoadX entry points
- `internal/artifacts/loader_test.go` (new) — per-repo-override + embedded-fallback dispatch tests
- `internal/artifacts/markdown.go` (new) — TOML-frontmatter parser (reuses nebula-phase parser shape)
- `internal/artifacts/markdown_test.go` (new) — frontmatter round-trip tests
- `internal/artifacts/expr.go` (new) — lexer + parser + evaluator (~600 LOC including tests)
- `internal/artifacts/expr_test.go` (new) — table-driven grammar tests, error position tests
- `internal/artifacts/types.go` (new) — Star, Skill, Constellation, SensorInstance + sub-types
- `internal/artifacts/types_test.go` (new) — DTO sanity tests
- `internal/artifacts/builtins.go` (new) — //go:embed defaults/**; embedded FS resolver
- `internal/artifacts/builtins_test.go` (new) — embedded-fs walk tests
- `internal/artifacts/defaults/` (new directory) — placeholder; gets filled in Phase 6 with the actual constellations/stars/skills
- `cmd/lint.go` (new) — quasar lint command
- `cmd/lint_test.go` (new) — table-driven validation cases
- `internal/arch_test/artifacts_test.go` (new) — expression grammar + schema strictness arch tests

## Acceptance Criteria

- [ ] `internal/artifacts/` package compiles
- [ ] `Loader.LoadStar("coder")` returns the per-repo `<repo>/stars/coder.md` if present, else the embedded default
- [ ] Star loading transitively resolves `skills:` references; the returned Star's Prompt is the concatenation of (skill prompt fragments) + (star body); Tools.Allowed is the union of base allowed + each skill's tools_add
- [ ] `Loader.LoadConstellation` returns a Constellation with all expressions pre-compiled (no evaluation deferred to runtime parse)
- [ ] `Loader.LoadSensorInstance` parses [config] as opaque map[string]any and does not validate against any sensor type schema (Configure does that)
- [ ] Expression parser supports: dot access, ==/!=/</<=/>/>=/&&/||/!/+/-/* / / / ternary / string interpolation / len() / has() / empty()
- [ ] Expression parser rejects function calls beyond the stdlib with a clear error
- [ ] `Expression.Eval(state)` returns the correct value for representative expressions
- [ ] `Expression.Eval` against a State missing a referenced field returns nil (zero value), not an error
- [ ] `quasar lint` exits 0 against a valid set of artifact files
- [ ] `quasar lint --strict` exits 1 when an unknown TOML key is present
- [ ] `quasar lint` reports edges referencing unknown nodes, stars referencing unknown skills, and sensor instances referencing unknown types
- [ ] DAG cycle detection in constellations fires when a constellation has a closed loop excluding terminal nodes (_done, _failed)
- [ ] Arch tests in `internal/arch_test/artifacts_test.go` pass
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all exit 0
