# Per-repo configuration

Audience: a developer adding Quasar to their repository for the first time.
Every repository Quasar manages carries its own configuration at the repo root,
layered over Quasar's embedded defaults. Nothing here is required beyond
`.quasar.yaml`; the embedded stars, skills, and constellations cover the common
case.

See [deployment.md](deployment.md) for registering the repo with a running
supervisor, and [safety.md](safety.md) for what Quasar is allowed to do once it
is pointed at your code.

## `.quasar.yaml` at the repo root

The minimum useful config:

```yaml
pre_commit:
  commands:
    - go vet ./...
    - go test -short ./...
  fail_on_error: true        # abort the commit if any command exits non-zero

budget:
  default_max_usd: 30.0      # per-nebula spend cap
  default_max_review_cycles: 5

branch:
  prefix: quasar/            # all Quasar branches start with this (enforced)
  base: main                 # what Quasar PRs target
```

The `[pre_commit]` block is loaded by the constellation runtime and passed into
every `gitops.Commit` call, so your quality gates run uniformly before any commit
Quasar makes — no constellation needs to know about them. There is no bypass.

**Secrets are never inlined.** Use `token_env` or `token_file` to reference a
credential; an inline `token: "..."` is a config-load error.

## The `sensors/` directory

One TOML file per sensor instance, each referencing a Go-registered sensor type
and supplying its config block. The GitHub issues sensor as a worked example:

```toml
# sensors/github-issues.toml
type = "github_issues"       # a built-in, Go-registered sensor type

[config]
owner = "papapumpkin"
repo  = "quasar"
labels = ["quasar"]          # only ingest issues with this label
poll_interval = "5m"
token_env = "QUASAR_GH_TOKEN"   # never inline the token
```

Each poll that finds a new matching issue creates a **seed nebula** in SQLite,
which surfaces in the TUI as an `awaiting_approval` draft.

## The `stars/`, `skills/`, and `constellations/` directories

These directories hold optional per-repo overrides:

- `stars/<name>.md` — Markdown + TOML frontmatter (Claude Code `SKILL.md`
  compatible).
- `skills/<name>.md` — Markdown + TOML frontmatter.
- `constellations/<name>.toml` — a DAG with edge conditions.

The resolver checks `<repo>/<kind>/<name>` first and falls back to the embedded
built-in of the same name. Authoring any of these is optional: if you write none,
Quasar uses its built-ins. Sensors are the exception — they have no embedded
defaults (only types are built-in), so a repo with no `sensors/` directory simply
ingests nothing automatically.

## Worked example

A complete repo config that polls GitHub issues labeled `quasar`, uses the
default stars, fires the architect on approval, and gates commits on
`go vet && go test -short`:

```
my-repo/
├── .quasar.yaml            # the pre_commit / budget / branch config above
└── sensors/
    └── github-issues.toml  # the github_issues sensor above
```

No `stars/`, `skills/`, or `constellations/` directory is needed — the embedded
defaults drive the architect → coder-reviewer → master-review lifecycle.

## Troubleshooting

| Symptom | Command |
|---------|---------|
| Static config issues | `quasar lint` |
| "Why didn't my issue ingest?" | `quasar sensor poll <repo> <sensor>` — force one poll |
| "Where did my nebula go?" | `quasar gc audit --since 1h` — read the GC audit trail |
| Credentials / git / pre-commit health | `quasar doctor` |
