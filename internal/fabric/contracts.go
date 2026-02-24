// Package fabric — contracts.go provides contract resolution for static analysis.
//
// ResolveContracts checks all phase contracts for completeness, identifying
// fulfilled, missing, and conflicting entanglements across the dependency graph.
package fabric

import (
	"fmt"
	"strings"
)

// ContractEntry represents a single producer-consumer relationship for an entanglement.
type ContractEntry struct {
	Consumer     string       // phase ID of the consumer
	Producer     string       // phase ID of the producer (empty if missing)
	Entanglement Entanglement // the entanglement being consumed or produced
}

// ContractReport summarises the resolution of all contracts.
type ContractReport struct {
	Fulfilled []ContractEntry // consumer expects X, producer provides X
	Missing   []ContractEntry // consumer expects X, no producer found
	Conflicts []ContractEntry // multiple producers for same symbol
	Warnings  []string        // ambiguous or weak matches
}

// ResolveContracts checks all contracts for completeness against the dependency
// graph. It returns a ContractReport with fulfilled, missing, and conflicting
// entries.
//
// deps maps phase ID to the list of phase IDs it depends on. This is used to
// determine which producers are visible to each consumer.
func ResolveContracts(contracts []PhaseContract, deps map[string][]string) *ContractReport {
	report := &ContractReport{}

	// Build a lookup: symbol key -> list of producer phase IDs.
	producersBySymbol := make(map[string][]producerInfo)
	for _, c := range contracts {
		for _, prod := range c.Produces {
			key := symbolKey(prod)
			producersBySymbol[key] = append(producersBySymbol[key], producerInfo{
				phaseID:      c.PhaseID,
				entanglement: prod,
			})
		}
	}

	// Check for conflicts: multiple producers for the same symbol.
	seen := make(map[string]bool)
	for key, producers := range producersBySymbol {
		if len(producers) > 1 && !seen[key] {
			seen[key] = true
			for _, p := range producers {
				report.Conflicts = append(report.Conflicts, ContractEntry{
					Producer:     p.phaseID,
					Entanglement: p.entanglement,
				})
			}
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("symbol %q produced by multiple phases: %s",
					key, joinProducerIDs(producers)))
		}
	}

	// Resolve consumer expectations.
	for _, c := range contracts {
		for _, consumed := range c.Consumes {
			key := symbolKey(consumed)
			producers, found := producersBySymbol[key]

			if !found || len(producers) == 0 {
				report.Missing = append(report.Missing, ContractEntry{
					Consumer:     c.PhaseID,
					Entanglement: consumed,
				})
				continue
			}

			// Check if the producer is a dependency of the consumer.
			consumerDeps := depsSet(deps, c.PhaseID)
			fulfilled := false
			for _, p := range producers {
				if consumerDeps[p.phaseID] {
					report.Fulfilled = append(report.Fulfilled, ContractEntry{
						Consumer:     c.PhaseID,
						Producer:     p.phaseID,
						Entanglement: consumed,
					})
					fulfilled = true
					break
				}
			}

			if !fulfilled {
				// Producer exists but is not a dependency of the consumer.
				report.Missing = append(report.Missing, ContractEntry{
					Consumer:     c.PhaseID,
					Entanglement: consumed,
				})
				report.Warnings = append(report.Warnings,
					fmt.Sprintf("consumer %q expects %q but producer %q is not a dependency",
						c.PhaseID, key, producers[0].phaseID))
			}
		}
	}

	return report
}

// symbolKey returns a unique key for an entanglement based on its kind,
// package, and name.
func symbolKey(e Entanglement) string {
	if e.Package != "" {
		return e.Kind + ":" + e.Package + "." + e.Name
	}
	return e.Kind + ":" + e.Name
}

// depsSet returns the transitive dependency set for a phase. For now it only
// returns direct dependencies; transitive closure can be added later.
func depsSet(deps map[string][]string, phaseID string) map[string]bool {
	set := make(map[string]bool)
	for _, d := range deps[phaseID] {
		set[d] = true
	}
	return set
}

// producerInfo pairs a phase ID with the entanglement it produces.
type producerInfo struct {
	phaseID      string
	entanglement Entanglement
}

// joinProducerIDs formats a slice of producerInfo as a comma-separated list
// of phase IDs.
func joinProducerIDs(producers []producerInfo) string {
	ids := make([]string, 0, len(producers))
	for _, p := range producers {
		ids = append(ids, p.phaseID)
	}
	return strings.Join(ids, ", ")
}
