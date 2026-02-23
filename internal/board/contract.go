package board

import (
	"fmt"
	"sort"
	"strings"
)

// BoardSnapshot is the full board state injected into a polling phase's context.
// It aggregates all fulfilled contracts, file claims, and phase progress so that
// a dependent phase can understand the interface surface available to it.
type BoardSnapshot struct {
	// Contracts holds all fulfilled contracts published by completed phases.
	Contracts []Contract

	// FileClaims maps filepath to the owning phase ID.
	FileClaims map[string]string

	// Completed lists phase IDs that have finished execution.
	Completed []string

	// InProgress lists phase IDs that are currently running.
	InProgress []string
}

// RenderSnapshot formats a BoardSnapshot into a human-readable string suitable
// for injection into an LLM prompt. Contracts are grouped by package and
// annotated with their producing phase.
func RenderSnapshot(snap BoardSnapshot) string {
	var b strings.Builder

	b.WriteString("## Board State\n")

	// Completed phases.
	b.WriteString("\n### Completed Phases\n")
	if len(snap.Completed) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, id := range snap.Completed {
			fmt.Fprintf(&b, "- %s\n", id)
		}
	}

	// Available contracts grouped by package.
	b.WriteString("\n### Available Contracts\n")
	if len(snap.Contracts) == 0 {
		b.WriteString("(none)\n")
	} else {
		grouped := groupContractsByPackage(snap.Contracts)
		pkgs := sortedKeys(grouped)
		for _, pkg := range pkgs {
			contracts := grouped[pkg]
			// Find the producer(s) for this package group.
			producers := uniqueProducers(contracts)
			fmt.Fprintf(&b, "#### %s (from: %s)\n", pkg, strings.Join(producers, ", "))
			for _, c := range contracts {
				if c.Signature != "" {
					fmt.Fprintf(&b, "- %s %s\n", c.Kind, c.Signature)
				} else {
					fmt.Fprintf(&b, "- %s %s\n", c.Kind, c.Name)
				}
			}
		}
	}

	// Active file claims.
	b.WriteString("\n### Active File Claims\n")
	if len(snap.FileClaims) == 0 {
		b.WriteString("(none)\n")
	} else {
		paths := sortedKeys(snap.FileClaims)
		for _, fp := range paths {
			fmt.Fprintf(&b, "- %s → %s\n", fp, snap.FileClaims[fp])
		}
	}

	// In-progress phases.
	b.WriteString("\n### In-Progress Phases\n")
	if len(snap.InProgress) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, id := range snap.InProgress {
			fmt.Fprintf(&b, "- %s\n", id)
		}
	}

	return b.String()
}

// groupContractsByPackage organises contracts by their Package field.
func groupContractsByPackage(contracts []Contract) map[string][]Contract {
	m := make(map[string][]Contract)
	for _, c := range contracts {
		m[c.Package] = append(m[c.Package], c)
	}
	return m
}

// uniqueProducers returns a deduplicated, sorted list of producer IDs from
// the given contracts.
func uniqueProducers(contracts []Contract) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, c := range contracts {
		if _, ok := seen[c.Producer]; !ok {
			seen[c.Producer] = struct{}{}
			out = append(out, c.Producer)
		}
	}
	sort.Strings(out)
	return out
}

// sortedKeys returns the keys of a string-keyed map in sorted order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
