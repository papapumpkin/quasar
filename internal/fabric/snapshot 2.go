package fabric

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Snapshot is the full fabric state injected into a scanning phase's context.
// It aggregates all fulfilled entanglements, file claims, phase progress, and
// unresolved discoveries so that a dependent phase can understand the interface
// surface and outstanding issues.
type Snapshot struct {
	// Entanglements holds all fulfilled entanglements published by completed phases.
	Entanglements []Entanglement

	// FileClaims maps filepath to the owning phase ID.
	FileClaims map[string]string

	// Completed lists phase IDs that have finished execution.
	Completed []string

	// InProgress lists phase IDs that are currently running.
	InProgress []string

	// Blocked lists phase IDs that are currently blocked.
	Blocked []string

	// UnresolvedDiscoveries holds discoveries that have not yet been resolved.
	UnresolvedDiscoveries []Discovery

	// Pulses holds shared execution context from completed/in-progress upstream tasks.
	Pulses []Pulse

	// PhaseStates maps phase ID to its current state (e.g. "running", "blocked").
	// Used to enrich file claim rendering with phase context.
	PhaseStates map[string]string

	// PhaseCycles maps phase ID to its current cycle number (e.g. 2 of 5).
	// Used to enrich file claim rendering.
	PhaseCycles map[string][2]int

	// Now overrides time.Now for deterministic rendering in tests.
	// If nil, time.Now is used.
	Now func() time.Time
}

// RenderSnapshot formats a Snapshot into a human-readable string suitable
// for injection into an LLM prompt. Entanglements are grouped by package and
// annotated with their producing phase and file locations.
func RenderSnapshot(snap Snapshot) string {
	var b strings.Builder

	// Summary line with counts for quick orientation.
	b.WriteString(renderSummaryLine(snap))

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
		renderEntanglements(&b, snap.Entanglements)
	}

	// Active file claims.
	b.WriteString("\n### Active File Claims\n")
	if len(snap.FileClaims) == 0 {
		b.WriteString("(none)\n")
	} else {
		renderFileClaims(&b, snap)
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

	// Unresolved discoveries.
	b.WriteString("\n### Unresolved Discoveries\n")
	if len(snap.UnresolvedDiscoveries) == 0 {
		b.WriteString("(none)\n")
	} else {
		renderDiscoveries(&b, snap)
	}

	// Shared context (pulses) grouped by kind.
	renderPulses(&b, snap.Pulses)

	return b.String()
}

// renderSummaryLine produces a one-line summary header with counts.
func renderSummaryLine(snap Snapshot) string {
	unresolved := len(snap.UnresolvedDiscoveries)
	return fmt.Sprintf("## Fabric State (%d completed, %d in-progress, %d blocked, %d entanglements, %d unresolved)\n",
		len(snap.Completed), len(snap.InProgress), len(snap.Blocked),
		len(snap.Entanglements), unresolved)
}

// renderEntanglements writes entanglements grouped by package with file locations.
func renderEntanglements(b *strings.Builder, entanglements []Entanglement) {
	grouped := groupEntanglementsByPackage(entanglements)
	pkgs := sortedKeys(grouped)
	for _, pkg := range pkgs {
		ents := grouped[pkg]
		producers := uniqueProducers(ents)
		fmt.Fprintf(b, "#### %s (from: %s)\n", pkg, strings.Join(producers, ", "))
		for _, e := range ents {
			label := e.Name
			if e.Signature != "" {
				label = e.Signature
			}
			if e.File != "" {
				fmt.Fprintf(b, "- %s %s (%s)\n", e.Kind, label, e.File)
			} else {
				fmt.Fprintf(b, "- %s %s\n", e.Kind, label)
			}
		}
	}
}

// renderFileClaims writes file claims with phase state and cycle context
// when available.
func renderFileClaims(b *strings.Builder, snap Snapshot) {
	paths := sortedKeys(snap.FileClaims)
	for _, fp := range paths {
		owner := snap.FileClaims[fp]
		suffix := renderClaimContext(owner, snap.PhaseStates, snap.PhaseCycles)
		fmt.Fprintf(b, "- %s → %s%s\n", fp, owner, suffix)
	}
}

// renderClaimContext builds the parenthetical context suffix for a file claim.
// Returns empty string when no enrichment data is available.
func renderClaimContext(owner string, states map[string]string, cycles map[string][2]int) string {
	if len(states) == 0 && len(cycles) == 0 {
		return ""
	}
	state := states[owner]
	cycle, hasCycle := cycles[owner]
	if state == "" && !hasCycle {
		return ""
	}
	var parts []string
	if state != "" {
		parts = append(parts, state)
	}
	if hasCycle {
		parts = append(parts, fmt.Sprintf("cycle %d/%d", cycle[0], cycle[1]))
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// renderDiscoveries writes unresolved discoveries with relative timestamps.
func renderDiscoveries(b *strings.Builder, snap Snapshot) {
	now := snap.now()
	for _, d := range snap.UnresolvedDiscoveries {
		age := relativeTime(now, d.CreatedAt)
		if d.Affects != "" {
			fmt.Fprintf(b, "- [%s] %s (from: %s, affects: %s, %s)\n",
				d.Kind, d.Detail, d.SourceTask, d.Affects, age)
		} else {
			fmt.Fprintf(b, "- [%s] %s (from: %s, %s)\n",
				d.Kind, d.Detail, d.SourceTask, age)
		}
	}
}

// renderPulses writes pulses grouped by kind for faster scanning.
func renderPulses(b *strings.Builder, pulses []Pulse) {
	if len(pulses) == 0 {
		return
	}

	grouped := make(map[string][]Pulse)
	for _, p := range pulses {
		grouped[p.Kind] = append(grouped[p.Kind], p)
	}

	b.WriteString("\n### Shared Context\n")

	// Render in deterministic order using the canonical pulse kinds,
	// then any remaining kinds sorted alphabetically.
	order := []string{PulseDecision, PulseFailure, PulseNote, PulseReviewerFeedback}
	rendered := make(map[string]bool)
	for _, kind := range order {
		ps, ok := grouped[kind]
		if !ok {
			continue
		}
		rendered[kind] = true
		writePulseGroup(b, kind, ps)
	}
	// Any remaining non-canonical kinds in sorted order.
	for _, kind := range sortedKeys(grouped) {
		if rendered[kind] {
			continue
		}
		writePulseGroup(b, kind, grouped[kind])
	}
}

// writePulseGroup writes a single pulse kind group.
func writePulseGroup(b *strings.Builder, kind string, pulses []Pulse) {
	fmt.Fprintf(b, "**%s:**\n", pulseKindTitle(kind))
	for _, p := range pulses {
		fmt.Fprintf(b, "- [%s] %s\n", p.TaskID, p.Content)
	}
}

// pulseKindTitle returns a human-readable title for a pulse kind.
func pulseKindTitle(kind string) string {
	switch kind {
	case PulseDecision:
		return "Decisions"
	case PulseFailure:
		return "Failures"
	case PulseNote:
		return "Notes"
	case PulseReviewerFeedback:
		return "Reviewer Feedback"
	default:
		if len(kind) == 0 {
			return kind
		}
		return strings.ToUpper(kind[:1]) + kind[1:]
	}
}

// now returns the current time, using snap.Now if set.
func (s Snapshot) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// relativeTime formats the duration between now and t as a human-readable
// relative timestamp (e.g. "just now", "3m ago", "1h ago").
func relativeTime(now, t time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// groupEntanglementsByPackage organizes entanglements by their Package field.
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
