package constellations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/telemetry"
)

// entanglementReader is the read seam the coordination Check consumes.
// *fabric.EntanglementStore satisfies it; tests inject a fake. Defined here
// (where consumed) per project convention, kept to the single method the Check
// needs so it cannot reach the rest of the store's lifecycle API.
type entanglementReader interface {
	ActiveAll(ctx context.Context) ([]fabric.Entanglement, error)
}

// Check inspects the entanglement table for symbols and packages that intersect
// a dispatching phase's scope and returns the sibling intents the coder needs to
// know about — in-flight signature drafts to conform to and deprecated symbols
// not to reintroduce. It is run before each coder dispatch.
//
// The check is advisory: a failure to read entanglements yields an error the
// caller logs and ignores, never failing the run. The coder might miss a note,
// but the merge gate still catches the conflict downstream.
type Check struct {
	// Store enumerates active entanglements. A nil Store disables the check.
	Store entanglementReader
	// Log records one summary row per check plus one row per override. Optional;
	// a nil Log skips telemetry.
	Log *telemetry.CoordinationLog
}

// PhaseContext is the scope of the phase about to dispatch, against which active
// entanglements are matched. Scope/Files drive the intersection test;
// SelfSymbols and the run/phase identity exclude the phase's own declarations;
// the Ignore* allowlists carry per-phase coordination overrides.
type PhaseContext struct {
	RunID       string   // current run; entanglements owned by it are excluded
	PhaseID     string   // current phase; its own declarations are excluded
	Scope       []string // glob patterns from the phase spec
	Files       []string // resolved file paths (used for symbol-name content match)
	SelfSymbols []string // symbols this phase already declared (never noted to itself)

	// IgnoreDeprecations suppresses deprecated-symbol notes for the listed names —
	// a phase whose purpose is to reintroduce a wrongly-removed symbol.
	IgnoreDeprecations []string
	// IgnoreSignatures suppresses in-flight signature notes for the listed names —
	// a phase that intentionally uses the prior, not the in-flight, signature.
	IgnoreSignatures []string
}

// Notes returns the coordination notes for active entanglements that intersect
// the phase's scope by symbol name (text match against the phase's files) or by
// package (glob overlap with the phase's scope), excluding the phase's own
// declarations. The result preserves ActiveAll's recency ordering, so the
// most-recently-updated intent — the signature a sibling is likely to ship —
// appears first.
//
// Per-phase overrides suppress matching notes and record an override row.
// Regardless of the note count, exactly one summary row is written per check.
func (c *Check) Notes(ctx context.Context, phase PhaseContext) ([]agent.CoordinationNote, error) {
	if c == nil || c.Store == nil {
		return nil, nil
	}
	ents, err := c.Store.ActiveAll(ctx)
	if err != nil {
		return nil, err
	}

	fileContents := readFiles(phase.Files)
	selfSymbols := toSet(phase.SelfSymbols)
	ignoreDeprecations := toSet(phase.IgnoreDeprecations)
	ignoreSignatures := toSet(phase.IgnoreSignatures)

	var notes []agent.CoordinationNote
	byStatus := map[string]int{}
	for _, e := range ents {
		if isSelf(e, phase, selfSymbols) {
			continue
		}
		if !intersectsScope(e, phase.Scope, fileContents) {
			continue
		}
		if symbol, reason, suppressed := overrideFor(e, ignoreDeprecations, ignoreSignatures); suppressed {
			if err := c.Log.RecordOverride(ctx, phase.PhaseID, symbol, reason); err != nil {
				logCoordinationErr("record override", err)
			}
			continue
		}
		notes = append(notes, toNote(e))
		byStatus[e.Status]++
	}

	if err := c.Log.RecordCheck(ctx, phase.RunID, phase.PhaseID, len(notes), byStatus); err != nil {
		logCoordinationErr("record check", err)
	}
	return notes, nil
}

// logCoordinationErr reports a non-fatal telemetry write failure to stderr.
// Coordination telemetry is a read-only side channel; a write failure must never
// fail the check or the run.
func logCoordinationErr(what string, err error) {
	fmt.Fprintf(os.Stderr, "coordination: %s: %v\n", what, err)
}

// isSelf reports whether the entanglement belongs to the dispatching phase and
// must not be noted back to it. A row matches self by run, by phase, or by a
// symbol name the phase already declared — covering both run-bound rows and the
// NULL-run rows the architect declares (which carry only PhaseID).
func isSelf(e fabric.Entanglement, phase PhaseContext, selfSymbols map[string]struct{}) bool {
	if phase.RunID != "" && e.RunID == phase.RunID {
		return true
	}
	if phase.PhaseID != "" && e.PhaseID == phase.PhaseID {
		return true
	}
	_, ok := selfSymbols[e.Name]
	return ok
}

// intersectsScope reports whether the entanglement is relevant to the phase:
// its symbol name appears as text in any of the phase's files (a cheap content
// match), or its package overlaps one of the phase's scope globs.
func intersectsScope(e fabric.Entanglement, scope []string, fileContents []string) bool {
	if e.Name != "" {
		for _, content := range fileContents {
			if strings.Contains(content, e.Name) {
				return true
			}
		}
	}
	return packageOverlapsScope(e.Package, scope)
}

// packageOverlapsScope reports whether a Go package path intersects any of the
// phase's scope globs. A glob's "**" / "*" wildcards are stripped to a directory
// prefix and matched against the package path; an exact filepath.Match is also
// tried so a precise glob still works. An empty package or empty scope never
// overlaps.
func packageOverlapsScope(pkg string, scope []string) bool {
	if pkg == "" {
		return false
	}
	for _, glob := range scope {
		if glob == "" {
			continue
		}
		if ok, _ := filepath.Match(glob, pkg); ok {
			return true
		}
		prefix := globPrefix(glob)
		if prefix != "" && (pkg == prefix || strings.HasPrefix(pkg, prefix+"/")) {
			return true
		}
	}
	return false
}

// globPrefix reduces a scope glob to its leading literal directory: the portion
// before the first wildcard, trailing slash trimmed. "internal/runtime/**" ->
// "internal/runtime"; "internal/*.go" -> "internal"; "**" -> "".
func globPrefix(glob string) string {
	if i := strings.IndexAny(glob, "*?["); i >= 0 {
		glob = glob[:i]
	}
	return strings.TrimRight(glob, "/")
}

// overrideFor reports whether the phase's [coordination] allowlists suppress the
// entanglement's note, returning the symbol and the reason for the audit row.
// Deprecated symbols are suppressed by ignore_deprecations; in-flight signatures
// by ignore_signatures.
func overrideFor(e fabric.Entanglement, ignoreDeprecations, ignoreSignatures map[string]struct{}) (symbol, reason string, suppressed bool) {
	if e.Status == fabric.StatusDeprecated {
		if _, ok := ignoreDeprecations[e.Name]; ok {
			return e.Name, "ignore_deprecations", true
		}
	}
	if e.Status == fabric.StatusInFlight {
		if _, ok := ignoreSignatures[e.Name]; ok {
			return e.Name, "ignore_signatures", true
		}
	}
	return "", "", false
}

// toNote projects an entanglement into the agent-layer coordination note,
// stamping the status-derived advice and the recency timestamp the operator
// audits.
func toNote(e fabric.Entanglement) agent.CoordinationNote {
	return agent.CoordinationNote{
		SiblingRunID:     e.RunID,
		SiblingPhaseID:   e.PhaseID,
		Kind:             e.Kind,
		Name:             e.Name,
		CurrentSignature: e.CurrentSignature,
		Status:           e.Status,
		Advice:           agent.AdviceForStatus(e.Status),
		Producer:         e.Producer,
		Package:          e.Package,
		DeclaredAt:       unixOrZero(e.DeclaredAt),
		UpdatedAt:        unixOrZero(recencyStamp(e)),
	}
}

// recencyStamp returns the entanglement's most-recent lifecycle timestamp, the
// same COALESCE order ActiveAll sorts by, so a note's UpdatedAt matches its rank.
func recencyStamp(e fabric.Entanglement) int64 {
	switch {
	case e.InFlightAt != 0:
		return e.InFlightAt
	case e.ClaimedAt != 0:
		return e.ClaimedAt
	default:
		return e.DeclaredAt
	}
}

// readFiles reads each path's contents, skipping any that cannot be read — the
// check is advisory, so a missing or unreadable file simply contributes no
// symbol matches rather than failing.
func readFiles(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		out = append(out, string(data))
	}
	return out
}

// toSet builds a lookup set from a slice, ignoring empty strings.
func toSet(items []string) map[string]struct{} {
	set := make(map[string]struct{}, len(items))
	for _, s := range items {
		if s != "" {
			set[s] = struct{}{}
		}
	}
	return set
}

// unixOrZero converts unix seconds to a time.Time, mapping 0 (unset) to the zero
// time so a never-stamped column round-trips as a zero Time rather than the
// epoch.
func unixOrZero(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
