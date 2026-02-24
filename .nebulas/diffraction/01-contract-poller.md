+++
id = "contract-poller"
title = "Deterministic contract-based Poller implementation"
type = "feature"
priority = 1
depends_on = []
scope = ["internal/fabric/contract_poller.go"]
+++

## Problem

The current dispatch gate uses `LLMPoller` — it sends the phase body and a rendered fabric snapshot to an LLM and asks "can you proceed?" This is:

- **Expensive**: One LLM call per phase per poll cycle. With 20 phases, that's 20 LLM calls just for scheduling decisions before any coding happens.
- **Non-deterministic**: The same board state can produce different dispatch decisions across runs.
- **Slow**: Each poll is a full LLM round-trip (~2-5 seconds), serializing the dispatch loop.
- **Overkill**: The question "are my consumed entanglements on the board?" is a set-intersection check, not a reasoning task.

The `static-scanner` (from observatory) already produces `PhaseContract` with `Produces` and `Consumes` lists. Checking whether consumed entanglements exist on the board is a map lookup, not an LLM call.

## Solution

### 1. New file: `internal/fabric/contract_poller.go`

```go
// ContractPoller implements the Poller interface using deterministic
// set-intersection checks. For each phase, it compares the phase's
// consumed entanglements (from static analysis) against what has been
// published to the fabric. No LLM calls are made.
type ContractPoller struct {
    // Contracts maps phase ID to its statically-derived contract.
    Contracts map[string]*PhaseContract

    // MatchMode controls how consumed entanglements are matched
    // against published ones. MatchExact requires name+kind+package
    // to match. MatchName requires only name+kind (looser).
    MatchMode MatchMode
}

type MatchMode int
const (
    MatchExact MatchMode = iota // name + kind + package
    MatchName                   // name + kind only
)
```

### 2. Poll implementation

```go
func (p *ContractPoller) Poll(ctx context.Context, phaseID string, snap FabricSnapshot) (PollResult, error) {
    contract, ok := p.Contracts[phaseID]
    if !ok {
        // No contract info — proceed optimistically (fail-open,
        // same as LLMPoller's malformed response behavior).
        return PollResult{Decision: PollProceed, Reason: "no contract registered"}, nil
    }

    if len(contract.Consumes) == 0 {
        // Phase doesn't consume anything — always proceed.
        return PollResult{Decision: PollProceed, Reason: "no consumed entanglements"}, nil
    }

    // Build a lookup index from the snapshot's published entanglements.
    published := indexEntanglements(snap.Entanglements, p.MatchMode)

    // Check each consumed entanglement against the index.
    var missing []string
    for _, consumed := range contract.Consumes {
        key := entanglementKey(consumed, p.MatchMode)
        if !published[key] {
            missing = append(missing, fmt.Sprintf("%s %s (%s)", consumed.Kind, consumed.Name, consumed.Package))
        }
    }

    if len(missing) == 0 {
        return PollResult{Decision: PollProceed, Reason: "all contracts fulfilled"}, nil
    }

    return PollResult{
        Decision:    PollNeedInfo,
        Reason:      fmt.Sprintf("%d/%d consumed entanglements missing", len(missing), len(contract.Consumes)),
        MissingInfo: missing,
    }, nil
}
```

### 3. File claim conflict detection

Beyond entanglement checks, also detect file claim conflicts:

```go
// Check for file claim conflicts with in-progress phases.
for _, scopePath := range contract.Scope {
    if owner, claimed := snap.FileClaims[scopePath]; claimed {
        return PollResult{
            Decision:     PollConflict,
            Reason:       fmt.Sprintf("file %s claimed by %s", scopePath, owner),
            ConflictWith: owner,
        }, nil
    }
}
```

### 4. Indexing

The entanglement index is built once per poll cycle (when the snapshot is constructed), not per phase:

```go
func indexEntanglements(entanglements []Entanglement, mode MatchMode) map[string]bool {
    idx := make(map[string]bool, len(entanglements))
    for _, e := range entanglements {
        idx[entanglementKey(e, mode)] = true
    }
    return idx
}

func entanglementKey(e Entanglement, mode MatchMode) string {
    switch mode {
    case MatchExact:
        return e.Kind + ":" + e.Package + ":" + e.Name
    case MatchName:
        return e.Kind + ":" + e.Name
    default:
        return e.Kind + ":" + e.Package + ":" + e.Name
    }
}
```

## Files

- `internal/fabric/contract_poller.go` — `ContractPoller`, `MatchMode`, `Poll()`, indexing helpers
- `internal/fabric/contract_poller_test.go` — Table-driven tests

## Acceptance Criteria

- [ ] `ContractPoller` implements `fabric.Poller` interface
- [ ] Returns `PollProceed` when all consumed entanglements are present on the board
- [ ] Returns `PollNeedInfo` with specific missing items when contracts are unfulfilled
- [ ] Returns `PollConflict` when scope files are claimed by in-progress phases
- [ ] Phases with no contract or no consumes always proceed (fail-open)
- [ ] `MatchExact` requires kind+package+name; `MatchName` requires kind+name only
- [ ] Zero LLM calls — pure map lookups
- [ ] Table-driven tests cover: all fulfilled, partial missing, all missing, no contract, file conflict, empty board
- [ ] `go vet ./...` passes
