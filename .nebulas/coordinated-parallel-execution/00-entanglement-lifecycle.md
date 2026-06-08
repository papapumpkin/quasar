+++
id = "entanglement-lifecycle"
title = "Six-state entanglement lifecycle: declared → claimed → in_flight → (fulfilled | withdrawn | deprecated); each transition wired to an observable runtime event"
type = "task"
priority = 1
scope = [
    "internal/fabric/migrations/**",
    "internal/fabric/entanglement_store.go",
    "internal/fabric/entanglement_store_test.go",
    "internal/neutron/**",
    "internal/constellations/runtime.go",
    "internal/constellations/operators.go",
]
+++

## Problem

Today's `entanglements` table records only a terminal moment: `status='pending'` becomes `'fulfilled'` once neutron sees the symbol committed. Two failures fall out:

1. **No pre-flight signal.** A new coder cannot ask "is anyone else modifying `Sensor.Poll` right now?" because in-flight intent is invisible — only completed work shows up.
2. **No deprecation signal.** When a producer phase *removes* a symbol, downstream consumers have no way to learn about it before they reintroduce a use of the removed symbol and trigger a post-merge build failure.

The fix is a lifecycle: each entanglement carries one of six statuses, and runtime events transition between them. This phase ships the schema, the store, the neutron emission timing, and the constellation-runtime hooks that perform the transitions.

## Solution

### Statuses

| Status | Meaning | Set when |
|---|---|---|
| `declared` | A phase's spec asserts it will produce / modify / deprecate this symbol | Architect operator at spec-parse time |
| `claimed` | A coder has picked up the phase and intends to act on the declaration | Coder pre-flight, before first edit |
| `in_flight` | A coder has produced at least one green build that touches this symbol | Every successful build inside a cycle |
| `fulfilled` | The phase's run terminated `done` and the symbol shipped | Supervisor on post-merge success |
| `withdrawn` | The phase's run terminated `failed` or `awaiting_human` | Supervisor on terminal failure |
| `deprecated` | The producer explicitly removed the symbol (diff deletes the declaration) | Neutron on diff containing a symbol deletion |

Statuses are not strictly linear — `deprecated` can follow `in_flight` (the producer changed their mind mid-cycle) or be the initial declaration (a phase whose entire purpose is removal).

### Schema additions

`internal/fabric/migrations/008_entanglement_lifecycle.sql`:

```sql
-- The current schema's UNIQUE (producer, kind, name) treats name + kind as
-- the identity. Lifecycle adds time-anchored bookkeeping so the TUI can
-- show "this was declared 4m ago by phase X" and the coordination
-- pre-flight can rank by recency.
ALTER TABLE entanglements ADD COLUMN run_id          TEXT;
ALTER TABLE entanglements ADD COLUMN phase_id        TEXT NOT NULL DEFAULT '';
ALTER TABLE entanglements ADD COLUMN declared_at     INTEGER;
ALTER TABLE entanglements ADD COLUMN claimed_at      INTEGER;
ALTER TABLE entanglements ADD COLUMN in_flight_at    INTEGER;
ALTER TABLE entanglements ADD COLUMN terminated_at   INTEGER;
ALTER TABLE entanglements ADD COLUMN current_signature TEXT;

CREATE INDEX entanglements_status_name
    ON entanglements (status, name);
CREATE INDEX entanglements_run
    ON entanglements (run_id);
CREATE INDEX entanglements_active
    ON entanglements (name, status)
    WHERE status IN ('declared', 'claimed', 'in_flight', 'deprecated');

-- Migrate prior 'pending' rows.
UPDATE entanglements
   SET status = 'fulfilled', terminated_at = strftime('%s', 'now')
 WHERE status = 'pending'
   AND producer IN (SELECT id FROM constellation_runs WHERE state = 'done');

UPDATE entanglements
   SET status = 'withdrawn', terminated_at = strftime('%s', 'now')
 WHERE status = 'pending'
   AND producer IN (SELECT id FROM constellation_runs WHERE state = 'failed');
```

The new `current_signature` column carries the symbol's signature at the most-recent in_flight update. That's what the coordination pre-flight (Phase 01) reads when injecting "use this signature" into a sibling's prompt.

### Store

`internal/fabric/entanglement_store.go`:

```go
type EntanglementStore struct{ db *sql.DB }

type Entanglement struct {
    Producer         string
    Consumer         string
    RunID            string
    PhaseID          string
    Kind             string  // "func" | "type" | "interface" | "table" | "var"
    Name             string
    Signature        string
    CurrentSignature string  // updated as the in_flight signature evolves
    Status           string
    Package          string
    DeclaredAt       int64
    ClaimedAt        int64
    InFlightAt       int64
    TerminatedAt     int64
}

// Declare records a producer's intent for a symbol. Called by the architect
// operator at spec-parse time. Idempotent on (producer, kind, name).
func (s *EntanglementStore) Declare(ctx context.Context, e Entanglement) error

// Claim transitions a declaration to claimed when a coder picks up the
// phase. No-op if the entanglement is not in 'declared'.
func (s *EntanglementStore) Claim(ctx context.Context, runID, phaseID, name string) error

// MarkInFlight records that a green build has touched the symbol with the
// given signature. The signature is what siblings see in their pre-flight
// coordination notes.
func (s *EntanglementStore) MarkInFlight(ctx context.Context, runID, name, signature string) error

// Deprecate records that the producer is removing the symbol. Called by
// neutron when a diff deletes the symbol's declaration.
func (s *EntanglementStore) Deprecate(ctx context.Context, runID, name string) error

// Fulfill transitions in_flight to fulfilled. Called by the supervisor on
// post-merge success.
func (s *EntanglementStore) Fulfill(ctx context.Context, runID string) error

// Withdraw transitions any non-terminal entanglements for a run to
// withdrawn. Called by the supervisor on terminal failure.
func (s *EntanglementStore) Withdraw(ctx context.Context, runID string) error

// Active returns entanglements whose status indicates in-flight intent
// (declared | claimed | in_flight | deprecated). Used by the pre-flight
// coordination check (Phase 01).
func (s *EntanglementStore) Active(ctx context.Context, name string) ([]Entanglement, error)
```

### Neutron emission timing

`internal/neutron/` already scans for symbols. Two extensions:

1. **Detect declarations from spec text.** When the architect parses a phase spec, neutron-style regex extraction pulls candidate symbol names from `## Files` and `## Solution` sections (e.g. `type Sensor interface`, `func (b *Budget) CheckBefore`). These become `declared` entanglements. False positives are OK — the lifecycle's later stages reconcile.

2. **Detect deletions in diffs.** On each cycle commit, walk the diff for removed top-level declarations. Emit `Deprecate` for each.

The diff walk is straightforward: any `-` line matching `^(func|type|var|const)\s+(\w+)` at column zero where no `+` line for the same symbol appears anywhere in the diff is a deletion. False positives (renames not detected as such) again degrade gracefully — the pre-flight check is advisory.

### Constellation-runtime hooks

Three insertion points in the runtime:

- `Runtime.Fire` for the architect constellation → after the architect parses phases, call `Declare` for each detected producer symbol
- `Runtime.dispatchStar` after a green build (via the existing pre-commit gate) → call `MarkInFlight` for each touched symbol with its current signature
- `Runtime.terminate` → call `Fulfill` (on `_done` for the merge gate's child) or `Withdraw` (on `_failed`)

The supervisor (post-merge, Phase 02 of this nebula) is the one that ultimately calls `Fulfill` after a successful merge — `dispatchStar` calling `Fulfill` would be premature.

### Backward compatibility

The migration's `UPDATE` clauses handle existing `pending` rows. Any code today querying `WHERE status = 'pending'` continues to work for one release cycle — we add `status = 'fulfilled' OR status = 'pending'` to those queries and remove `pending` references after the next nebula.

### Tests

- `entanglement_store_test.go` — full lifecycle: Declare → Claim → MarkInFlight (multiple updates) → Fulfill; Declare → Withdraw; Deprecate ordering
- `entanglement_store_test.go` — Idempotency: Declare same (producer, kind, name) twice is a no-op, not an error
- `entanglement_store_test.go` — Active query returns only `declared | claimed | in_flight | deprecated` rows
- `neutron_test.go` — spec-text declaration extraction on the architect's input
- `neutron_test.go` — diff-walking deletion detection (positive + negative cases)
- Integration: a fixture run that flows from Declare through Fulfill end-to-end; assert all transitions appear in the row's timestamps

## Files

- `internal/fabric/migrations/008_entanglement_lifecycle.sql` (new)
- `internal/fabric/entanglement_store.go` (rewrite — old store was minimal)
- `internal/fabric/entanglement_store_test.go` (new)
- `internal/neutron/diff_walk.go` (new) — deletion detector
- `internal/neutron/diff_walk_test.go` (new)
- `internal/neutron/spec_extract.go` (new) — declaration detector from spec text
- `internal/neutron/spec_extract_test.go` (new)
- `internal/constellations/runtime.go` (modify) — wire Declare / MarkInFlight / Fulfill / Withdraw at the three insertion points
- `internal/constellations/operators.go` (modify) — architect operator calls Declare per detected symbol

## Acceptance Criteria

- [ ] Migration 008 adds the lifecycle columns + indexes without breaking existing rows
- [ ] Existing `pending` rows migrate to `fulfilled` or `withdrawn` based on their run's terminal state
- [ ] `EntanglementStore.Declare` is idempotent on (producer, kind, name)
- [ ] `EntanglementStore.MarkInFlight` updates `current_signature` and `in_flight_at` atomically
- [ ] `EntanglementStore.Deprecate` transitions any in-flight entanglement to `deprecated` for the symbol
- [ ] `EntanglementStore.Active(name)` returns only rows whose status is in {declared, claimed, in_flight, deprecated}
- [ ] Neutron extracts declared producer symbols from `## Files` and `## Solution` sections of the architect's input
- [ ] Neutron's diff walker detects top-level `func|type|var|const` deletions and emits Deprecate
- [ ] Runtime.Fire (architect path) writes Declare rows for every detected producer symbol
- [ ] Runtime emits MarkInFlight for symbols touched in a green build
- [ ] Runtime emits Withdraw on terminal failure
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
