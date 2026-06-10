# Development

Audience: a **human contributor** who knows Go and SQLite but is new to Quasar.
This is the human-facing handbook. [CLAUDE.md](../CLAUDE.md) is the *agent*
handbook — the rules an LLM follows when authoring nebulas and editing this
repo. Where the two overlap (Go conventions, nebula authoring), this page links
to CLAUDE.md rather than restating it.

## Contents

1. [Local setup](#local-setup)
2. [Build and test commands](#build-and-test-commands)
3. [Repo layout](#repo-layout)
4. [The arch tests](#the-arch-tests)
5. [Style](#style)
6. [Adding a sub-package](#adding-a-sub-package)
7. [Adding a constellation](#adding-a-constellation)
8. [Adding a star or skill](#adding-a-star-or-skill)
9. [Working with the fabric](#working-with-the-fabric)
10. [Working with the bus](#working-with-the-bus)
11. [Writing a nebula spec](#writing-a-nebula-spec)
12. [The autorun preference](#the-autorun-preference)

## Local setup

```bash
go build -o quasar .     # build the binary into ./quasar
go test ./...            # run every test, including the arch tests
```

The binary is self-contained — defaults (stars, skills, constellations,
migrations) are `go:embed`-ed. At runtime Quasar writes its state to a single
SQLite database (WAL mode) and a content-addressed blob store; in a deployed
install these live under `/var/lib/quasar/` (see [deployment.md](deployment.md)
for the canonical layout). Tests that need a database create a temporary one per
test — see [Working with the fabric](#working-with-the-fabric).

## Build and test commands

```bash
go build ./...               # compile everything
go test ./...                # all tests
go test ./internal/loop/...  # one package
go vet ./...                 # static analysis
bash scripts/lint.sh         # the project linter (must exit 0 before commit)
```

The arch tests live in [`internal/arch_test/`](../internal/arch_test/) and run
as part of `go test ./...`. They are ordinary Go tests that parse the rest of
the tree, so a structural violation fails the build like any other test.

## Repo layout

`cmd/` holds one Cobra command per file. `internal/` holds the engine. `docs/`
is this documentation set. The packages under `internal/`:

| Package | Responsibility |
|---------|----------------|
| `internal/agent` | Agent roles (coder, reviewer, …) and the `Invoker` interface; context-budget and health-policy types. |
| `internal/ansi` | ANSI escape-code constants and helpers for terminal styling. |
| `internal/artifacts` | The embedded default stars/skills/constellations, the artifact loader, and the per-repo override resolver. |
| `internal/blobstore` | Content-addressed (SHA-256) blob store: large diffs/outputs stored once, zstd-compressed, tracked for GC. |
| `internal/board` | Reserved stub package. |
| `internal/bus` | Typed in-process publish/subscribe event bus decoupling producers (workers) from consumers (TUI, telemetry). |
| `internal/checkpoint` | Serializable coder-reviewer loop state (cycle, phase, SHA) so a run can resume after a restart. |
| `internal/claude` | Runs the `claude` CLI as a subprocess, parses its JSON, and applies context budgets and the dead-coder healthcheck. |
| `internal/config` | Viper-backed `.quasar.yaml` loading; the `Config` struct is the canonical config surface (see [configuration.md](configuration.md)). |
| `internal/constellations` | The runtime DAG engine: Fire/Step walk, operators, budget + cycle guard, and nested-constellation dispatch. |
| `internal/dag` | Generic DAG primitives: topological sort, cycle detection, impact scoring. |
| `internal/fabric` | The SQLite persistence layer (`constellation_runs`, `star_invocations`, nebulas, …); see [fabric.md](fabric.md). |
| `internal/filter` | Deterministic pre-reviewer checks (build/vet/lint/test) that fail fast before invoking the reviewer agent. |
| `internal/forge` | Write-side adapter to code hosts; deliberately a near-empty surface (`Name` only) until the PR-creation nebula lands. |
| `internal/gc` | The garbage collector: the only code path that hard-deletes lifecycle rows and unreferenced blobs (see [gc.md](gc.md)). |
| `internal/gitops` | The git output-safety perimeter: every git write routes through here (see [safety.md](safety.md)). |
| `internal/loop` | The legacy in-process coder-reviewer loop still backing `quasar run`; superseded by the coder-reviewer constellation. |
| `internal/nebula` | Multi-phase nebula orchestration: parse, validate, plan, branch, and execute workers. |
| `internal/neutron` | Epoch archival: snapshots active fabric state into standalone SQLite files and purges for the next epoch. |
| `internal/repos` | Multi-repo foundation: the SQLite repo registry and the resolver that layers per-repo config over defaults. |
| `internal/sensors` | The poll-driven `Sensor` boundary to external trackers; the GitHub adapter lives in `sensors/github` (see [sensors.md](sensors.md)). |
| `internal/snapshot` | Tree/file snapshot rendering used by the TUI and diffing. |
| `internal/telemetry` | Structured JSONL event stream of state transitions, agent invocations, and cache metrics. |
| `internal/tui` | The Bubble Tea terminal UI; reads state **only** through the fabric (enforced by an arch test). |
| `internal/tycho` | The DAG scheduler that observes fabric state to resolve eligible tasks (topo-sorted, impact-ranked). |
| `internal/ui` | The stderr `Printer` for coder-reviewer loop output, wrapping the `ansi` constants. |

> Keep this table in sync with `ls internal/`. It is the canonical expansion of
> the Project Structure block in [CLAUDE.md](../CLAUDE.md); when they disagree,
> this page wins and CLAUDE.md should be updated to match.

## The arch tests

The tests in [`internal/arch_test/`](../internal/arch_test/) encode
architectural invariants as build-time assertions. Each enforces a rule that
prose alone could not keep true:

| Test file | Enforces |
|-----------|----------|
| `artifacts_test.go` | The constellation expression language stays minimal: only documented operators, only the `len`/`has`/`empty` stdlib functions, and strict-mode schema rejection of unknown keys. |
| `blobref_test.go` | Every `*_blob_hash` SQL column is registered with the blobstore, so the mark-and-sweep GC never reclaims a blob that a live row still references. |
| `boundaries_test.go` | Layer boundaries: stars do not import sensors, the TUI is DB-only (no runtime/gc/concrete-sensor imports), and GC reads time through an injected clock — never `time.Now()`. |
| `globals_test.go` | No mutable package-level globals beyond an allowlist (error sentinels, interface checks, `regexp.MustCompile`, sync primitives, simple/composite literals). |
| `godoc_test.go` | Every exported symbol in `internal/` carries a GoDoc comment beginning with the symbol name. |
| `helpers_test.go` | Sanity tests for the shared arch-test helpers (`internalPackages`, `goFilesIn`, `importsOf`, `lineCount`, …) so the other tests can't pass vacuously. |
| `integrations_test.go` | Sensor layering (no package imports the concrete `sensors/github` adapter), no inline `token:` in committed configs, and the `Forge` interface stays minimal (exactly one method, `Name`). |
| `interfaces_test.go` | Interfaces are defined where they are **consumed**, not where they are implemented, except for an explicit allowlist of justified co-locations. |
| `layering_test.go` | The import graph respects a layered DAG: a package may import only its own layer or below, and every package has an assigned layer. |
| `runtime_helpers_test.go` / `safety_test.go` | The git/gh safety perimeter: no direct `git` exec outside `internal/gitops`, no `gh` exec outside `sensors/github` and `forge`, and no destructive git subcommands (`--force` without lease, `reset --hard`, `rebase -i`, ref deletion). Gated by `QUASAR_ARCH_TEST_GIT_WALL`. |
| `size_test.go` | File and package size caps: **≤ 400 lines** per non-test `.go` file (`maxLinesPerFile`) and **≤ 20** non-test files per package (`maxFilesPerPackage`), with a logged exception list for files mid-decomposition. |
| `statetoml_test.go` | No production code writes a `state.toml`: SQLite is the single source of nebula state (the on-disk `nebula.state.toml` authoring file is a deliberately different name and is exempt). |
| `stdout_test.go` | Every `os.Stdout`/`fmt.Print*` in `cmd/` carries an explicit `// arch-test: stdout-allowed` marker, keeping stdout a clean machine-readable channel and human output on stderr. |

When you add a package, file, or git/gh call site that one of these guards, the
test will tell you exactly which invariant you crossed. Prefer fixing the
structure over adding to the allowlist; the allowlists exist for in-progress
migrations, and each entry carries a TODO.

## Style

The Go conventions are documented once, in the **Go Conventions** section of
[CLAUDE.md](../CLAUDE.md). The points that bite a new contributor first:

- **Interface placement** — define an interface in the package that *consumes*
  it (enforced by `interfaces_test.go`).
- **Error handling** — handle every error explicitly with `%w`-wrapped context;
  never `_ = expr` an error away. Sentinel errors are package-level vars.
- **Function size** — keep functions short and single-purpose; extract helpers
  for distinct phases.
- **Output destinations** — human-readable output goes to **stderr** via
  `ui.Printer`; **stdout** is reserved for structured/machine-readable data and
  requires the `// arch-test: stdout-allowed` marker (enforced by
  `stdout_test.go`).
- **Context** — every method doing I/O takes `context.Context` first and uses
  `exec.CommandContext`.

## Adding a sub-package

1. Create `internal/<name>/` with a package doc comment (`godoc_test.go`
   requires GoDoc on every exported symbol).
2. Assign it a layer in `layering_test.go` — `TestNoUnknownPackages` fails
   until every package has one. Import only from your layer or below.
3. Keep tests in the same package (`package <name>`) unless you deliberately
   need black-box `_test`.
4. Stay under the size caps (`size_test.go`): ≤ 400 lines/file, ≤ 20
   files/package.
5. If your package shells out to git or `gh`, route through `internal/gitops`
   or `internal/sensors/github` — the safety tests reject direct exec.

## Adding a constellation

1. Drop a TOML file in
   [`internal/artifacts/defaults/constellations/`](../internal/artifacts/defaults/constellations/).
2. Run `quasar lint` to validate the node/edge/expression syntax. The
   expression language is intentionally minimal — `artifacts_test.go` enforces
   the allowed operators and the `len`/`has`/`empty` functions.
3. Write at least one runtime test that walks the DAG (Fire → Step → terminate).
   See [constellations.md](constellations.md) and [runtime.md](runtime.md).

## Adding a star or skill

Same pattern as constellations, in
[`internal/artifacts/defaults/stars/`](../internal/artifacts/defaults/stars/) or
`.../skills/`: a Markdown file with TOML frontmatter (Claude Code `SKILL.md`
compatible). `quasar lint` validates the frontmatter; a per-repo file of the
same name under `<repo>/stars|skills/` overrides the embedded default.

## Working with the fabric

[`internal/fabric`](../internal/fabric/) is the SQLite persistence layer. Prefer
a typed store method over a raw query; reach for direct SQL only for one-off
diagnostics. Tests open a temporary database rather than touching a shared one —
follow the test-DB pattern in
[`internal/fabric/sqlite.go`](../internal/fabric/sqlite.go) and its tests. The
TUI may read fabric state but nothing else (the DB-only rule in
`boundaries_test.go`). See [fabric.md](fabric.md) for the schema.

## Working with the bus

[`internal/bus`](../internal/bus/) is an in-process pub/sub bus. Publish a typed
event when a producer (worker, loop) reaches a state a consumer (TUI, telemetry)
cares about; subscribe to react. `bus.Publish` is fire-and-forget — a publish
error must be logged, not propagated, so a missing subscriber never blocks the
producer.

## Writing a nebula spec

Nebulas are multi-phase task specs under `.nebulas/<name>/` (a `nebula.toml`
manifest plus `*.md` phase files). The full field-by-field reference is the
**Nebula Authoring** section of [CLAUDE.md](../CLAUDE.md); validate with
`quasar nebula validate .nebulas/<name>`. The manifest's config surface is
catalogued in [configuration.md](configuration.md#per-nebula-toml).

## The autorun preference

Quasar runs in `gate = "trust"` (autorun) only; the manual review/approve/watch
modes have been removed. Never author a nebula with a non-trust gate. See
[configuration.md](configuration.md#per-nebula-toml) for the manifest surface.
