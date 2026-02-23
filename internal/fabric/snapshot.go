package fabric

import (
	"fmt"
	"sort"
	"strings"
)

// FabricSnapshot is the full fabric state injected into a scanning phase's context.
// It aggregates all fulfilled entanglements, file claims, and phase progress so that
// a dependent phase can understand the interface surface available to it.
type FabricSnapshot struct {
	// Entanglements holds all fulfilled entanglements published by completed phases.
	Entanglements []Entanglement

	// FileClaims maps filepath to the owning phase ID.
	FileClaims map[string]string

	// Completed lists phase IDs that have finished execution.
	Completed []string

	// InProgress lists phase IDs that are currently running.
	InProgress []string
}

// RenderSnapshot formats a FabricSnapshot into a human-readable string suitable
// for injection into an LLM prompt. Entanglements are grouped by package and
// annotated with their producing phase.
func RenderSnapshot(snap FabricSnapshot) string {
	var b strings.Builder

	b.WriteString("## Fabric State\n")

	// Completed phases.
	b.WriteString("\n### Completed Phases\n")
	if len(snap.Completed) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, id := range snap.Completed {
			fmt.Fprintf(&b, "- %s\n", id)
		}
	}

	// Available entanglements grouped by package.
	b.WriteString("\n### Available Entanglements\n")
	if len(snap.Entanglements) == 0 {
		b.WriteString("(none)\n")
	} else {
		grouped := groupEntanglementsByPackage(snap.Entanglements)
		pkgs := sortedKeys(grouped)
		for _, pkg := range pkgs {
			entanglements := grouped[pkg]
			// Find the producer(s) for this package group.
			producers := uniqueProducers(entanglements)
			fmt.Fprintf(&b, "#### %s (from: %s)\n", pkg, strings.Join(producers, ", "))
			for _, e := range entanglements {
				if e.Signature != "" {
					fmt.Fprintf(&b, "- %s %s\n", e.Kind, e.Signature)
				} else {
					fmt.Fprintf(&b, "- %s %s\n", e.Kind, e.Name)
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

// groupEntanglementsByPackage organises entanglements by their Package field.
func groupEntanglementsByPackage(entanglements []Entanglement) map[string][]Entanglement {
	m := make(map[string][]Entanglement)
	for _, e := range entanglements {
		m[e.Package] = append(m[e.Package], e)
	}
	return m
}

// uniqueProducers returns a deduplicated, sorted list of producer IDs from
// the given entanglements.
func uniqueProducers(entanglements []Entanglement) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, e := range entanglements {
		if _, ok := seen[e.Producer]; !ok {
			seen[e.Producer] = struct{}{}
			out = append(out, e.Producer)
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
