+++
id = "catalog-reports"
title = "Generate rich reports from the nebula catalog"
type = "feature"
priority = 2
depends_on = ["nebula-scanner"]
+++

## Problem

The catalog data is only useful if it can be presented in ways that serve different audiences: a human skimming the project timeline, an AI agent onboarding into the codebase, or a maintainer tracking what areas have been evolving.

## Solution

### Report Interface

Use the Strategy pattern (same philosophy as the DAG engine's `ReportStrategy`):

```go
// ReportFormat defines how catalog data is rendered.
type ReportFormat interface {
    Render(catalog *Spacetime) (string, error)
}
```

### Built-in Formats

#### 1. Timeline Report (`timeline`)

A chronological narrative of the repo's evolution:

```markdown
# Quasar Evolution Timeline

## 1. tui-landing-page (feature) — completed
Built the TUI home screen with bubbletea, showing project health and nebula status.
- Areas: internal/tui, cmd
- Phases: 5/5 completed
- Jan 15 – Feb 10, 2026

## 2. dag-engine (feature) — planned
DAG-based task dependency engine with topological sort, impact scoring, and parallel tracks.
- Areas: internal/dag
- Phases: 0/5 completed
- Enables: nebula-wiring
```

#### 2. Area Heatmap (`heatmap`)

Shows which packages have been most active and through which nebulas:

```markdown
# Codebase Area Heatmap

| Package | Nebulas Touching | Last Changed | Category Mix |
|---------|-----------------|--------------|--------------|
| internal/tui | 2 | tui-landing-page | feature |
| internal/dag | 1 | dag-engine | feature |
| cmd | 3 | tui-landing-page, ... | mixed |
```

#### 3. Dependency Graph (`graph`)

ASCII or structured representation of how nebulas relate:

```markdown
# Nebula Dependency Graph

tui-landing-page
dag-engine → nebula-wiring
relativity (standalone)
```

#### 4. Structured JSON (`json`)

Machine-readable output of the full catalog for external tooling or AI consumption:

```json
{
  "version": 1,
  "nebulas": [
    {"name": "tui-landing-page", "sequence": 1, "status": "completed", ...}
  ]
}
```

#### 5. Onboarding Brief (`onboarding`)

A prose summary designed to be pasted into an AI agent's context window:

```markdown
# Project Onboarding: Quasar

This project has evolved through 3 nebulas (2 completed, 1 planned).

The codebase started with a TUI layer (tui-landing-page), then designed a
DAG engine for task scheduling (dag-engine). The relativity system you're
reading was built to track this evolution.

Key areas and their history:
- internal/tui: Built in nebula 1, provides the terminal UI
- internal/dag: Designed in nebula 2, handles task dependency graphs
- internal/relativity: Built in nebula 3, this catalog system

Active work: dag-engine is planned with 5 phases covering topological sort,
Union-Find partitioning, PageRank scoring, and facade/strategy patterns.
```

### Output Destinations

Reports can be written to:
- stdout (for piping/scripting)
- A file path (e.g., `--output=ONBOARDING.md`)
- stderr with UI formatting (for human reading in terminal)

## Files

- `internal/relativity/report.go` — ReportFormat interface
- `internal/relativity/report_timeline.go` — timeline report
- `internal/relativity/report_heatmap.go` — area heatmap
- `internal/relativity/report_graph.go` — dependency graph
- `internal/relativity/report_json.go` — structured JSON
- `internal/relativity/report_onboarding.go` — AI onboarding brief
- `internal/relativity/report_test.go` — tests for each format

## Acceptance Criteria

- [ ] All 5 report formats produce meaningful output
- [ ] Timeline is chronologically ordered
- [ ] Heatmap correctly aggregates area usage across nebulas
- [ ] JSON output is valid and parseable
- [ ] Onboarding brief reads as coherent prose, not a data dump
- [ ] Reports work with 0 nebulas (empty state), 1 nebula, and many
- [ ] `go test ./internal/relativity/...` passes