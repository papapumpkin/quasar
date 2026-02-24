+++
id = "plan-command"
title = "Add nebula plan CLI command"
type = "feature"
priority = 2
depends_on = ["plan-engine"]
scope = ["cmd/nebula_plan.go", "internal/ui/plan.go"]
+++

## Problem

There's no way to preview what `nebula apply` will do before it starts burning budget. The existing flow is: load -> validate -> build bead plan -> apply beads -> (optionally) start workers. The `gatePlan` step shows execution waves, but it's a simple text dump with no contract analysis, no risk detection, and no structured output.

Terraform solved this with `terraform plan` — a dry run that shows exactly what will be created, modified, and destroyed before `terraform apply` touches anything. Quasar needs the same: `quasar nebula plan <dir>` should show the full execution graph with contracts, risks, and diffs against prior plans.

## Solution

### 1. New Cobra command: `cmd/nebula_plan.go`

Register under the `nebula` command group:

```go
var nebulaPlanCmd = &cobra.Command{
    Use:   "plan <nebula-dir>",
    Short: "Preview the execution plan for a nebula",
    Long:  "Analyzes a nebula's phases, dependencies, and entanglement contracts without executing anything. Shows the full execution graph, identifies risks, and optionally diffs against a prior plan.",
    Args:  cobra.ExactArgs(1),
    RunE:  runNebulaPlan,
}
```

### 2. Flags

```
--json           Output the plan as JSON instead of human-readable
--save           Save the plan to <nebula-dir>/<name>.plan.json
--diff           Diff against a previously saved plan
--no-color       Disable ANSI colors
```

### 3. Human-readable output format

Render to stderr via a new `ui/plan.go` printer:

```
Observatory: relativity
==============================

Execution Graph:
  Wave 1: spacetime-model
  Wave 2: spacetime-lock, nebula-scanner
  Wave 3: catalog-reports
  Wave 4: agent-synthesis
  Wave 5: cli-relativity

Tracks: 1 (max parallelism: 1)
  Track 0: spacetime-model -> spacetime-lock -> nebula-scanner -> ...

Contracts:
  spacetime-model PRODUCES:
    type SpacetimeManifest (spacetime)
    type NebulaEntry (spacetime)
    func LoadManifest (spacetime)
  nebula-scanner CONSUMES:
    type SpacetimeManifest (spacetime) <- spacetime-model [fulfilled]
    type NebulaEntry (spacetime) <- spacetime-model [fulfilled]

Risks:
  [warning] Single track detected — max_workers=1 limits parallelism
  [info] Phase spacetime-lock has no detected consumers

Stats:
  Phases: 6 | Waves: 5 | Tracks: 1 | Parallel factor: 1
  Contracts: 8 fulfilled, 0 missing, 0 conflicts
  Budget cap: $50.00

Plan saved to .nebulas/relativity/relativity.plan.json
```

### 4. JSON output

When `--json` is passed, emit the `ExecutionPlan` struct as JSON to stdout (machine-readable). This follows the convention that stdout is for structured data.

### 5. Diff output

When `--diff` is passed and a previous plan exists:

```
Plan diff: relativity
  + Phase agent-synthesis added
  ~ Phase nebula-scanner: 2 new produces (ScanResult, ScanOptions)
  - Contract catalog-reports <- nebula-scanner/ScanOptions removed
  ! New risk: scope overlap between agent-synthesis and cli-relativity
```

## Files

- `cmd/nebula_plan.go` — Cobra command definition and `runNebulaPlan` handler
- `cmd/nebula.go` — Register `nebulaPlanCmd` as subcommand (add to init)
- `internal/ui/plan.go` — Stderr rendering for execution plans, risks, diffs, and stats

## Acceptance Criteria

- [ ] `quasar nebula plan .nebulas/relativity` outputs the execution graph, contracts, risks, and stats
- [ ] `--json` outputs the plan as valid JSON to stdout
- [ ] `--save` writes `<name>.plan.json` to the nebula directory
- [ ] `--diff` compares against a saved plan and shows changes
- [ ] Human-readable output uses ANSI colors (respecting `--no-color`)
- [ ] Command is registered under `quasar nebula` and appears in `--help`
- [ ] Exit code is non-zero if error-severity risks are detected
- [ ] `go build` and `go vet ./...` pass
