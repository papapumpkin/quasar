<div align="center">
<pre>
                      .·::··::··::·.                    .·::··::··::·.
                  .::··::::::::::::··::.            .::··::::::::::::··::.
              .::··::::.    ··::||::··  ··::    ::··  ··::||::··    .::::··::.
           .::··:::.   ..··::::\||/::··    ··::··    ··::\||/::::··..   .:::··::.
        .::··:::.  ..··::::..   \||/  ..::··  ··::..  \||/   ..::::··..  .:::··::.
      .::··::. ..··::::..  ··::. \|/ .::··::····::··::. \|/ .::··  ..::::··.. .::··::.
    .::··::. .··::::. .··::.. ·:. | .:·::··:.  .::··::·. | .:· ..::··. .::::··. .::··::.
   .::·::. .··:::.  ··::.. ··::. | .::··  ::··::  ··::. | .::·· ..::··  .:::··. .::·::.
  .::·::. .··:::.  ··::. .··::.. | ..::··. .::··. .::··.. | ..::··. .::··  .:::··. .::·::.
  ::·::. .··:::.  ··::..··::. .  | . .::··:..::··:..::··. | . .::··..::··  .:::··. .::·::
  :·::..··:::.  ··::..··::..··:. | .::··..::····::..::··. | .:·..::··..::··  .:::··..::·:
  :·::.··:::. ··::..··::. ··::.. | ..::··. .:·::·:. .::··.. | ..::·· .::··..::·· .:::··.::·:
  :·:.··:::. ··::..··::..··::..--<b>-@-</b>--..::··..::::..::··..--<b>-@-</b>--..::··..::··..::·· .:::··.:·:
  :·::.··:::. ··::..··::. ··::.. | ..::··. .:·::·:. .::··.. | ..::·· .::··..::·· .:::··.::·:
  :·::..··:::.  ··::..··::..··:. | .::··..::····::..::··. | .:·..::··..::··  .:::··..::·:
  ::·::. .··:::.  ··::..··::. .  | . .::··:..::··:..::··. | . .::··..::··  .:::··. .::·::
  .::·::. .··:::.  ··::. .··::.. | ..::··. .::··. .::··.. | ..::··. .::··  .:::··. .::·::.
   .::·::. .··:::.  ··::.. ··::. | .::··  ::··::  ··::. | .::·· ..::··  .:::··. .::·::.
    .::··::. .··::::. .··::.. ·:. | .:·::··:.  .::··::·. | .:· ..::··. .::::··. .::··::.
      .::··::. ..··::::..  ··::. /|\ .::··::····::··::. /|\ .::··  ..::::··.. .::··::.
        .::··:::.  ..··::::..   /||\  ..::··  ··::..  /||\   ..::::··..  .:::··::.
           .::··:::.   ..··::::/||\ ::··    ··::··    ··::/||\::::··..   .:::··::.
              .::··::::.    ··::||::··  ··::    ::··  ··::||::··    .::::··::.
                  .::··::::::::::::··::.            .::··::::::::::::··::.
                      .·::··::··::·.                    .·::··::··::·.

                                   Q    U    A    S    A    R
</pre>
</div>

# Quasar

Quasar is a multi-repo coding agent **coordinator**. It watches the
repositories you register, turns external signals (today, GitHub issues) into
draft units of work called *nebulas*, and — once you approve one — drives a
team of LLM agents through a declarative workflow that writes code, reviews its
own diff, commits to an isolated `quasar/*` branch, and opens a pull request.
Every run is recorded in a local SQLite database so a long-lived process can
crash and resume exactly where it stopped.

What makes it more than a wrapper around `claude -p`:

- **Declarative workflows, not a hardcoded loop.** Agents and their wiring live
  in TOML *constellations* you can override per repo; the coder→reviewer→revise
  loop and the outer master-review loop are both data, enforced by one runtime
  back-edge counter — no special-cased Go control flow.
- **A safety perimeter on every write.** All git writes route through one
  wrapper that refuses to push anywhere outside the `quasar/*` namespace and
  refuses destructive operations. The agent cannot touch `main`.
- **Durable, multi-repo coordination.** Runs, phases, nebulas, blobs, and
  cross-phase *entanglements* are persisted in SQLite, so Quasar runs as an
  always-on fleet service across many repositories — not one task at a time.

## Prerequisites

- [**Claude Code CLI**](https://docs.anthropic.com/en/docs/claude-code)
  (`claude`) — installed and authenticated. Quasar shells out to it for every
  agent invocation.
- [**Go**](https://go.dev/) 1.25+ — to build from source.
- [**git**](https://git-scm.com/) — Quasar drives a vanilla `git` binary for
  all repository writes.
- [**GitHub CLI**](https://cli.github.com/) (`gh`) — required for the GitHub
  sensor (reading issues) and for opening pull requests.

## Install

```bash
git clone https://github.com/papapumpkin/quasar.git
cd quasar
go build -o quasar .
```

Optionally install to your `$GOPATH/bin`:

```bash
go install .
```

Verify the toolchain and configuration any time with `quasar doctor`.

## Quick Start

Quasar's primary mode is the **fleet**: a long-running view over every repo you
register. The shortest path from zero to a running nebula:

```bash
# 1. Register a repository for Quasar to operate on.
quasar repo register /path/to/your/repo

# 2. Author a nebula (a multi-phase task spec) inside that repo.
#    A nebula is a directory of one manifest + one Markdown file per phase.
mkdir -p /path/to/your/repo/.nebulas/my-task
$EDITOR /path/to/your/repo/.nebulas/my-task/nebula.toml   # see CLAUDE.md for the format

# 3. Open the fleet dashboard and watch it work.
quasar fleet
```

In the fleet view, sensor-produced drafts and imported nebulas appear in the
**awaiting-approval** lane. Press `a` to approve one: Quasar enqueues an
*architect* run that decomposes the draft into phases, then drives each phase
through the coder-reviewer loop and a final master-review before opening a PR.
In-flight runs and recently completed nebulas occupy the other two lanes.

For a single repo without the fleet, `quasar nebula apply .nebulas/my-task
--auto` runs a nebula directly through the legacy in-process engine.

## Core Concepts

One sentence each; follow the link for the canonical definition and code
location.

- **[Nebula](docs/glossary.md#nebula)** — a multi-phase task specification (a
  manifest plus one Markdown file per phase).
- **[Phase](docs/glossary.md#phase)** — one file inside a nebula, ultimately one
  unit of coder-reviewer work.
- **[Constellation](docs/glossary.md#constellation)** — a declarative workflow
  DAG, written in TOML, that wires agents and builtins into a run.
- **[Star](docs/glossary.md#star)** — an LLM agent character defined as
  Markdown + TOML (the coder, the reviewer, the architect, …).
- **[Skill](docs/glossary.md#skill)** — a reusable prompt fragment + tool grant
  that composes into a star.
- **[Sensor](docs/glossary.md#sensor)** — a poll-driven adapter that turns
  external signals (GitHub issues, …) into seed nebulas.
- **[Fabric](docs/glossary.md#fabric)** — the SQLite-backed persistence layer
  for runs, phases, nebulas, and blobs.
- **[Entanglement](docs/glossary.md#entanglement)** — a producer's declared,
  in-flight, or terminal claim on a symbol that other phases coordinate around.

The full vocabulary lives in **[docs/glossary.md](docs/glossary.md)**.

## CLI Reference

| Command         | What it does                                                                 |
|-----------------|------------------------------------------------------------------------------|
| `init`          | Scaffold a `.quasar.yaml` in the current directory.                          |
| `doctor`        | Diagnose configuration, integrations, credentials, and git setup.            |
| `validate`      | Alias for `quasar doctor`.                                                    |
| `fleet`         | Launch the multi-repo fleet dashboard (alias: `quasar tui`).                  |
| `repo`          | Register and manage the repositories Quasar operates on.                     |
| `sensor`        | Sensor administration — force a single poll cycle for debugging.             |
| `nebula apply`  | Run a nebula directly through the legacy in-process engine.                  |
| `gc`            | Garbage-collect completed nebulas, runs, blobs, and stale worktrees.        |
| `lint`          | Validate artifact files (constellations, stars, skills, sensors).            |

Each command's full flag set is available via `quasar <command> --help`. The
layered design behind these commands is described in
[docs/architecture.md](docs/architecture.md), and the docs index
([docs/README.md](docs/README.md)) maps every subsystem to its deep-dive
document.

## Where to Read Next

- **[docs/architecture.md](docs/architecture.md)** — the big picture: the four
  layers (operator surface, orchestration, coordination + safety, effectors),
  with flow walkthroughs from approval to pull request.
- **[docs/glossary.md](docs/glossary.md)** — the vocabulary, alphabetized, each
  term tied to its canonical code location.
- **[docs/README.md](docs/README.md)** — the index of the entire `docs/` tree,
  classified into start-here / mechanics / safety / operations / extension /
  contributor / historical.

## Contributing

The developer handbook — build and test commands, package layout, Go
conventions, and the rules for authoring nebulas — lives in
[CLAUDE.md](CLAUDE.md).

## License

MIT — see [LICENSE.md](LICENSE.md).
