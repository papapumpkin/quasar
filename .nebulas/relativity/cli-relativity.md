+++
id = "cli-relativity"
title = "Add quasar relativity CLI commands"
type = "feature"
priority = 3
depends_on = ["catalog-reports", "agent-synthesis"]
+++

## Problem

The catalog, reports, and synthesizer need a CLI surface so users and CI can invoke them. This is the user-facing entry point for the relativity system.

## Solution

### Command: `quasar relativity`

Top-level subcommand with the following verbs:

#### `quasar relativity scan`

Run the scanner and update `.relativity/spacetime.toml`:

```bash
quasar relativity scan              # scan and update spacetime.toml
quasar relativity scan --dry-run    # show what would change without writing
```

#### `quasar relativity report <format>`

Generate a report in the specified format:

```bash
quasar relativity report timeline        # chronological narrative
quasar relativity report heatmap         # area activity heatmap
quasar relativity report graph           # nebula dependency graph
quasar relativity report json            # machine-readable JSON
quasar relativity report onboarding      # AI onboarding brief
quasar relativity report --all           # all formats to stdout
quasar relativity report timeline -o timeline.md  # write to file
```

#### `quasar relativity synthesize`

Generate agent-consumable artifacts:

```bash
quasar relativity synthesize             # generate all artifacts
quasar relativity synthesize --memory    # only memory files
quasar relativity synthesize --claude-md # only CLAUDE.md section
quasar relativity synthesize --prompt    # only onboarding prompt
quasar relativity synthesize --dry-run   # preview without writing
```

#### `quasar relativity status`

Quick summary of the catalog state:

```bash
$ quasar relativity status
Quasar Relativity — 3 nebulas tracked
  completed: 1 (tui-landing-page)
  in_progress: 1 (dag-engine)
  planned: 1 (relativity)
Last scan: 2026-02-18T12:00:00Z
```

### Cobra Integration

Follow the existing pattern: one file per command in `cmd/`, functionality in `internal/relativity/`.

```go
// cmd/relativity.go — parent command
// cmd/relativity_scan.go
// cmd/relativity_report.go
// cmd/relativity_synthesize.go
// cmd/relativity_status.go
```

### Flags

| Flag | Commands | Description |
|------|----------|-------------|
| `--dry-run` | scan, synthesize | Preview changes without writing |
| `--output, -o` | report | Write to file instead of stdout |
| `--all` | report | Generate all report formats |
| `--format` | report | Alias for the positional format argument |
| `--memory` | synthesize | Only generate memory files |
| `--claude-md` | synthesize | Only generate CLAUDE.md section |
| `--prompt` | synthesize | Only generate onboarding prompt |

## Files

- `cmd/relativity.go` — parent `relativity` command
- `cmd/relativity_scan.go` — scan subcommand
- `cmd/relativity_report.go` — report subcommand
- `cmd/relativity_synthesize.go` — synthesize subcommand
- `cmd/relativity_status.go` — status subcommand

## Acceptance Criteria

- [ ] `quasar relativity scan` populates `.relativity/spacetime.toml`
- [ ] `quasar relativity report <format>` produces each of the 5 formats
- [ ] `quasar relativity synthesize` generates agent artifacts
- [ ] `quasar relativity status` shows a quick summary
- [ ] `--dry-run` previews without writing
- [ ] `--output` flag writes to file
- [ ] All commands follow existing Cobra patterns
- [ ] `go test ./cmd/...` passes