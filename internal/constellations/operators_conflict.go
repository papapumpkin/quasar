package constellations

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// Registered builtin names the merge-conflict-resolve constellation routes
// through. (emit_conflict_telemetry lives in operators_conflict_telemetry.go.)
const (
	opRenderConflictContextName      = "render_conflict_context"
	opConflictResolutionDecisionName = "conflict_resolution_decision"
	conflictResolutionSchemaName     = "conflict-resolution-result-v1"
	conflictResolverStatusResolved   = "resolved"
	conflictResolverStatusHuman      = "needs_human"
)

// conflictModeMarkers / conflictModeNoMarkers are the two collision modes the
// renderer and resolver share. markers carries conflicted files with <<<<<<<
// markers; no_markers carries a post-merge build failure with no markers.
const (
	conflictModeMarkers   = "markers"
	conflictModeNoMarkers = "no_markers"
)

// configFileBasenames are files whose conflicts always escalate to a human:
// their semantics reach beyond the textual diff (dependency graphs, run config),
// so a mechanical three-way merge is unsafe. Matched case-insensitively on the
// path's basename.
var configFileBasenames = map[string]bool{
	".quasar.yaml":      true,
	".quasar.yml":       true,
	"nebula.toml":       true,
	"nebula.state.toml": true,
	"go.mod":            true,
	"go.sum":            true,
	"package.json":      true,
	"package-lock.json": true,
	"cargo.toml":        true,
	"cargo.lock":        true,
	"pyproject.toml":    true,
	"tsconfig.json":     true,
}

// protectedPathPrefixes are the source roots where a delete-vs-modify collision
// always escalates: deleting a file another workstream still edits under these
// trees is a structural decision a human must ratify.
var protectedPathPrefixes = []string{"internal/", "cmd/"}

// workstream is one side of a collision: the originating run/phase, its spec
// (Problem/Solution), its diff against base, and the entanglements it emitted.
type workstream struct {
	Label         string // "A" or "B"
	RunID         string
	PhaseID       string
	Title         string
	Problem       string
	Solution      string
	Diff          string
	Entanglements []fabric.Entanglement
}

// conflictContext is the fully assembled input the conflict-resolver star reads.
// renderConflictContext turns it into the single markdown block the star
// consumes; keeping assembly (operator) separate from rendering (pure function)
// makes the deterministic output unit-testable without a runtime.
type conflictContext struct {
	Mode        string
	A           workstream
	B           workstream
	Files       []string // conflicted files, markers mode
	BuildOutput string   // post-merge build failure, no_markers mode
	Worktree    string
}

// opRenderConflictContext assembles the conflict-resolver's prompt context from
// the merge gate's hand-off inputs (mode, conflicted files or build output,
// worktree) plus best-effort enrichment of each workstream's spec, diff, and
// entanglements. Output: {"context": <markdown>}.
//
// The src_*/dst_* spec and diff inputs are threaded by the supervisor that fires
// the merge gate (deferred — see merge-gate.toml). Until then the operator
// degrades gracefully: a workstream with no spec/diff renders "(not provided)"
// rather than failing, so the resolver still gets the collision signal and any
// recorded entanglements.
func opRenderConflictContext(ctx context.Context, rt *Runtime, _ *State, args map[string]any) (map[string]any, error) {
	mode := strings.TrimSpace(stringArg(args, "mode"))
	if mode != conflictModeMarkers && mode != conflictModeNoMarkers {
		return nil, fmt.Errorf("render_conflict_context: mode must be %q or %q (got %q)",
			conflictModeMarkers, conflictModeNoMarkers, mode)
	}
	cc := conflictContext{
		Mode:        mode,
		Files:       toStringSlice(args["files"]),
		BuildOutput: stringArg(args, "build_output"),
		Worktree:    stringArg(args, "worktree"),
		A:           rt.buildWorkstream(ctx, "A", args, "src_"),
		B:           rt.buildWorkstream(ctx, "B", args, "dst_"),
	}
	return map[string]any{"context": renderConflictContext(cc)}, nil
}

// buildWorkstream reads one side's descriptor inputs (under the given prefix,
// e.g. "src_") and attaches the entanglements that side's run/phase emitted.
func (r *Runtime) buildWorkstream(ctx context.Context, label string, args map[string]any, prefix string) workstream {
	ws := workstream{
		Label:    label,
		RunID:    stringArg(args, prefix+"run_id"),
		PhaseID:  stringArg(args, prefix+"phase_id"),
		Title:    stringArg(args, prefix+"title"),
		Problem:  stringArg(args, prefix+"problem"),
		Solution: stringArg(args, prefix+"solution"),
		Diff:     stringArg(args, prefix+"diff"),
	}
	ws.Entanglements = r.entanglementsFor(ctx, ws.RunID, ws.PhaseID)
	return ws
}

// entanglementsFor returns the active entanglements emitted by a run or phase,
// sorted by name for deterministic rendering. Best-effort and nil-safe: a
// runtime with no entanglement store, or a query error, yields no rows rather
// than failing the render — the collision signal is still useful without them.
func (r *Runtime) entanglementsFor(ctx context.Context, runID, phaseID string) []fabric.Entanglement {
	if r.entanglements == nil || (runID == "" && phaseID == "") {
		return nil
	}
	all, err := r.entanglements.ActiveAll(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "constellations: render_conflict_context entanglements: %v\n", err)
		return nil
	}
	var out []fabric.Entanglement
	for _, e := range all {
		if (runID != "" && e.RunID == runID) || (phaseID != "" && e.PhaseID == phaseID) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// renderConflictContext renders the assembled context into the single markdown
// block the conflict-resolver star reads. It is pure (no I/O), so its output is
// deterministic for a given conflictContext and directly unit-testable.
func renderConflictContext(cc conflictContext) string {
	var b strings.Builder
	renderWorkstream(&b, cc.A)
	b.WriteString("\n")
	renderWorkstream(&b, cc.B)

	b.WriteString("\n## How they collided\n\n")
	fmt.Fprintf(&b, "- Mode: %s\n", cc.Mode)
	if cc.Mode == conflictModeNoMarkers {
		b.WriteString("- Build error output:\n\n```\n")
		b.WriteString(orPlaceholder(cc.BuildOutput, "(no build output captured)"))
		b.WriteString("\n```\n")
	} else {
		fmt.Fprintf(&b, "- Conflicted files: %s\n", joinOr(cc.Files, "(none reported)"))
	}

	b.WriteString("\n## What you must do\n\n")
	b.WriteString("1. Preserve A's intent for the modified contract\n")
	b.WriteString("2. Preserve B's intent for the consumer code\n")
	b.WriteString("3. Reconcile the contract between them per the conflict-resolution-rules skill — do not pick a winner\n")
	return b.String()
}

// renderWorkstream renders one side's header, spec, diff, and entanglements.
func renderWorkstream(b *strings.Builder, ws workstream) {
	fmt.Fprintf(b, "## Workstream %s (run %s, phase %s, %q)\n",
		ws.Label, orPlaceholder(ws.RunID, "unknown"), orPlaceholder(ws.PhaseID, "unknown"),
		orPlaceholder(ws.Title, "untitled"))
	fmt.Fprintf(b, "### Spec — Problem\n%s\n", orPlaceholder(ws.Problem, "(not provided)"))
	fmt.Fprintf(b, "### Spec — Solution\n%s\n", orPlaceholder(ws.Solution, "(not provided)"))
	b.WriteString("### Diff against base\n```diff\n")
	b.WriteString(orPlaceholder(ws.Diff, "(not provided)"))
	b.WriteString("\n```\n")
	fmt.Fprintf(b, "### Entanglements emitted by %s\n", ws.Label)
	b.WriteString(renderEntanglements(ws.Entanglements))
}

// renderEntanglements renders an entanglement list as one bullet per symbol,
// preferring the most recent (current) signature. An empty list renders a
// placeholder so the section is never silently absent.
func renderEntanglements(ents []fabric.Entanglement) string {
	if len(ents) == 0 {
		return "(none recorded)\n"
	}
	var b strings.Builder
	for _, e := range ents {
		sig := e.CurrentSignature
		if sig == "" {
			sig = e.Signature
		}
		if sig != "" {
			fmt.Fprintf(&b, "- %s (%s, %s, signature=%q)\n", e.Name, e.Kind, e.Status, sig)
		} else {
			fmt.Fprintf(&b, "- %s (%s, %s)\n", e.Name, e.Kind, e.Status)
		}
	}
	return b.String()
}

// opConflictResolutionDecision parses the conflict-resolver star's JSON output
// (args["output"], schema conflict-resolution-result-v1) into the fields the
// merge-conflict-resolve constellation routes on, after applying the universal
// escalation guards that force a human review independent of what the resolver
// claimed.
//
// Output: {"status": resolved|needs_human, "build_passed": bool,
// "files_changed": int, "escalation_reason": <string|"">}. The constellation
// commits on status=resolved && build_passed, loops within the cycle cap on
// resolved && !build_passed, and routes needs_human (or an exhausted cap) to
// give_up → _awaiting_human.
//
// The config-file and delete-vs-modify guards short-circuit to needs_human
// before the resolver output is even parsed: those collisions are never safe to
// auto-resolve, so a malformed or over-confident resolver payload cannot override
// the escalation. A field-path error is returned only when no guard fired and
// the output violates the schema.
func opConflictResolutionDecision(_ context.Context, _ *Runtime, _ *State, args map[string]any) (map[string]any, error) {
	if reason := escalationReason(toStringSlice(args["files"]), toStringSlice(args["delete_modify"])); reason != "" {
		return conflictDecisionOutput(conflictResolverStatusHuman, false, 0, reason), nil
	}

	raw, ok := args["output"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("conflict_resolution_decision: missing string input %q", "output")
	}
	dec, err := parseConflictResolution(raw)
	if err != nil {
		return nil, err
	}
	return conflictDecisionOutput(*dec.Status, *dec.BuildPassed, len(dec.FilesChanged),
		derefOr(dec.EscalationReason, "")), nil
}

// escalationReason returns a non-empty reason when a universal escalation guard
// fires: any conflicted file is a config file, or any delete-vs-modify collision
// lands on a protected source path. An empty string means no guard fired.
func escalationReason(conflictedFiles, deleteModifyFiles []string) string {
	for _, f := range conflictedFiles {
		if isConfigFile(f) {
			return fmt.Sprintf("config-file conflict on %q requires human review", f)
		}
	}
	for _, f := range deleteModifyFiles {
		if isProtectedPath(f) {
			return fmt.Sprintf("delete-vs-modify collision on protected path %q requires human review", f)
		}
	}
	return ""
}

// isConfigFile reports whether path's basename is a config file whose conflicts
// always escalate. Matching is case-insensitive and basename-only, so a config
// file in any directory is caught.
func isConfigFile(p string) bool {
	if strings.TrimSpace(p) == "" {
		return false
	}
	normalized := strings.ReplaceAll(p, "\\", "/")
	return configFileBasenames[strings.ToLower(path.Base(normalized))]
}

// isProtectedPath reports whether path lives under a protected source root.
func isProtectedPath(p string) bool {
	clean := strings.TrimPrefix(strings.ReplaceAll(p, "\\", "/"), "./")
	for _, prefix := range protectedPathPrefixes {
		if strings.HasPrefix(clean, prefix) {
			return true
		}
	}
	return false
}

// conflictResolution is the typed form of the conflict-resolver star's JSON
// output. Pointers distinguish an omitted required field from one supplied empty
// so a missing field is reported with its path.
type conflictResolution struct {
	Status           *string  `json:"status"`
	FilesChanged     []string `json:"files_changed"`
	BuildPassed      *bool    `json:"build_passed"`
	EscalationReason *string  `json:"escalation_reason"`
}

// parseConflictResolution decodes and validates a raw payload against schema
// conflict-resolution-result-v1, returning a field-path error on the first
// violation. Split out so the validation rules are unit-testable without a
// runtime.
func parseConflictResolution(raw string) (*conflictResolution, error) {
	var dec conflictResolution
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&dec); err != nil {
		return nil, fmt.Errorf("conflict_resolution_decision: parse %s: %w", conflictResolutionSchemaName, err)
	}
	if err := validateConflictResolution(&dec); err != nil {
		return nil, err
	}
	return &dec, nil
}

// validateConflictResolution enforces the schema's required fields and closed
// value set. Every error names the offending field's path.
func validateConflictResolution(dec *conflictResolution) error {
	if dec.Status == nil {
		return conflictSchemaErr("status", "required")
	}
	if *dec.Status != conflictResolverStatusResolved && *dec.Status != conflictResolverStatusHuman {
		return conflictSchemaErr("status", "must be resolved or needs_human (got %q)", *dec.Status)
	}
	if dec.BuildPassed == nil {
		return conflictSchemaErr("build_passed", "required")
	}
	if *dec.Status == conflictResolverStatusHuman && strings.TrimSpace(derefOr(dec.EscalationReason, "")) == "" {
		return conflictSchemaErr("escalation_reason", "required when status is needs_human")
	}
	return nil
}

// conflictSchemaErr builds a uniform field-path error against the conflict
// schema, sharing fieldSchemaErr with the other decision operators.
func conflictSchemaErr(field, format string, args ...any) error {
	return fieldSchemaErr("conflict_resolution_decision", conflictResolutionSchemaName, field, format, args...)
}

// conflictDecisionOutput builds the operator's output map in one place so the
// escalation and parse paths emit the same shape.
func conflictDecisionOutput(status string, buildPassed bool, filesChanged int, reason string) map[string]any {
	return map[string]any{
		"status":            status,
		"build_passed":      buildPassed,
		"files_changed":     filesChanged,
		"escalation_reason": reason,
	}
}

// stringArg returns args[key] as a string, or "" when absent or not a string.
func stringArg(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

// toStringSlice coerces a node input into a []string, tolerating both the
// []string a Go caller passes and the []any a TOML round-trip (child input
// seeding) produces. Non-string elements and non-slice values yield an empty
// slice.
func toStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// orPlaceholder returns the trimmed string, or the placeholder when it is empty.
func orPlaceholder(s, placeholder string) string {
	if strings.TrimSpace(s) == "" {
		return placeholder
	}
	return s
}

// joinOr joins items with ", ", or returns the placeholder when the list is
// empty.
func joinOr(items []string, placeholder string) string {
	if len(items) == 0 {
		return placeholder
	}
	return strings.Join(items, ", ")
}
