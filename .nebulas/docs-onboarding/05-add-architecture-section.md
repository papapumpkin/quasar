+++
id = "add-architecture-section"
title = "Add Architecture & Concepts section for advanced features"
type = "task"
priority = 2
depends_on = ["update-project-structure"]
scope = ["README.md"]
allow_scope_overlap = true
+++

## Problem

Quasar has grown a substantial coordination and observability layer — Fabric, Entanglements, Claims, Discoveries, Pulses, Telemetry, Filter, Neutron, Tycho, DAG — none of which are mentioned in the README. A user who encounters these terms in CLI help, error messages, or the TUI has no reference point.

These are advanced features that most new users won't need immediately, but they should at least be introduced so users know they exist and what vocabulary means.

## Solution

Add an "Architecture" or "Concepts" section after the Nebula Blueprints section. Use a glossary/table format to briefly explain each concept. This section acts as a map — not a manual.

Suggested content:

```
## Concepts

Quasar uses a cosmic vocabulary for its internal systems:

| Concept | Package | Description |
|---------|---------|-------------|
| **Fabric** | `internal/fabric` | SQLite-based shared state store for multi-phase coordination |
| **Entanglement** | `internal/fabric` | Exported type/function signature posted by one phase, visible to others |
| **Claim** | `internal/fabric` | Exclusive file ownership lock held by a phase during execution |
| **Discovery** | `internal/fabric` | Issue surfaced by an agent (conflict, ambiguity, missing dep) |
| **Pulse** | `internal/fabric` | Timestamped note, decision, or failure emitted during execution |
| **Filter** | `internal/filter` | Pre-reviewer deterministic checks (build, vet, lint, test) |
| **Tycho** | `internal/tycho` | DAG scheduler resolving phase execution order and eligibility |
| **Neutron** | `internal/neutron` | Archived epoch snapshot (standalone SQLite file) |
| **Epoch** | — | A single execution run; archived to a neutron on completion |
| **Telemetry** | `internal/telemetry` | JSONL event stream for state transitions and auditability |
| **Hail** | `internal/fabric` | Human interrupt signal surfaced as a discovery |
```

Keep it to the table plus 1-2 introductory sentences. Don't explain how to use the `fabric` CLI or telemetry viewer — that's what `--help` and future docs are for.

## Files

- `README.md` — add Concepts section

## Acceptance Criteria

- [ ] New "Concepts" section exists in the README
- [ ] All canonical vocabulary terms are listed with one-line descriptions
- [ ] Section is concise — table format, no paragraphs per concept
- [ ] Positioned after the Nebula section so new users encounter it after learning the basics
