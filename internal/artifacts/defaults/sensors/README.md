# Sensors have no embedded defaults

Unlike constellations, stars, and skills — each of which ships a set of
built-in defaults embedded in the Quasar binary — **sensors have no embedded
default instances.** Only sensor *types* are built in (they are implemented in
Go and registered at startup, e.g. the GitHub issue/PR poller).

A sensor only does something once a repo *configures an instance* of a built-in
type. Instances are authored per-repo as TOML files under:

    <repo>/sensors/<name>.toml

Each instance references a Go-registered sensor type and supplies that type's
config block. For example, a GitHub sensor instance:

    type = "github"

    [config]
    repo        = "papapumpkin/quasar"
    labels      = ["quasar"]
    poll_period = "5m"
    token_env   = "GITHUB_TOKEN"

There is deliberately nothing to embed here: a default sensor instance would
have to name a concrete external resource (a specific repo, a specific token),
which is repo-specific by definition. This directory exists only to document
that absence — the loader finds no built-in sensors and looks solely at
`<repo>/sensors/`.

## Secrets

Tokens must never appear inline. Use `token_env` (read from an environment
variable) or `token_file` (read from a file path). An inline `token = "..."`
is a config-load error.
