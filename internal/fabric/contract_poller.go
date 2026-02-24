package fabric

import (
	"context"
	"fmt"
)

// MatchMode controls how consumed entanglements are matched against
// published ones during contract polling.
type MatchMode int

const (
	// MatchExact requires kind+package+name to match.
	MatchExact MatchMode = iota

	// MatchName requires only kind+name (looser, ignores package).
	MatchName
)

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

// Poll checks whether phaseID has enough context from the fabric to run
// by performing deterministic set-intersection checks against the phase's
// contract. It returns PollProceed when all consumed entanglements are
// present, PollNeedInfo when entanglements are missing, or PollConflict
// when scope files are claimed by another phase.
func (p *ContractPoller) Poll(_ context.Context, phaseID string, snap FabricSnapshot) (PollResult, error) {
	contract, ok := p.Contracts[phaseID]
	if !ok {
		// No contract info — proceed optimistically (fail-open,
		// same as LLMPoller's malformed response behavior).
		return PollResult{Decision: PollProceed, Reason: "no contract registered"}, nil
	}

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

// indexEntanglements builds a set of entanglement keys from a slice,
// using the specified match mode. The index is built once per poll cycle.
func indexEntanglements(entanglements []Entanglement, mode MatchMode) map[string]bool {
	idx := make(map[string]bool, len(entanglements))
	for _, e := range entanglements {
		idx[entanglementKey(e, mode)] = true
	}
	return idx
}

// entanglementKey produces a lookup key for an entanglement based on the
// match mode. MatchExact uses kind:package:name; MatchName uses kind:name.
func entanglementKey(e Entanglement, mode MatchMode) string {
	switch mode {
	case MatchName:
		return e.Kind + ":" + e.Name
	default:
		return e.Kind + ":" + e.Package + ":" + e.Name
	}
}
