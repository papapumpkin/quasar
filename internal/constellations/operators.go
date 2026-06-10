package constellations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/gitops"
	"github.com/papapumpkin/quasar/internal/neutron"
)

// opCommitName is the registered name of the commit builtin. The dispatch loop
// keys its post-commit in-flight hook off it.
const opCommitName = "commit"

// neutronKinds maps the Go keyword neutron reports to the fabric entanglement
// kind the lifecycle stores. Fabric has no dedicated var/const kinds yet, so
// both fold into KindType. That is intentionally lossy: because identity is
// (producer, kind, name), a phase that declares both `var Foo` and `const Foo`
// (or `type Foo` + `var Foo`) collapses to one (phase, type, Foo) row and the
// later Declare is swallowed by ON CONFLICT DO NOTHING. The collision is rare,
// entanglements are advisory, and adding distinct kinds is premature until
// var/const coordination is actually needed downstream.
var neutronKinds = map[string]string{
	"func":  fabric.KindFunction,
	"type":  fabric.KindType,
	"var":   fabric.KindType,
	"const": fabric.KindType,
}

// opRenderSeedPrompt renders a seed nebula into a Markdown brief the architect
// star consumes. It reads the nebula snapshot already in State, so it makes no
// database call. The repo's [pre_commit] commands are appended so the architect
// plans phases that anticipate the quality bar the coder will be measured
// against. Output: {"prompt": <markdown>}.
func opRenderSeedPrompt(_ context.Context, rt *Runtime, st *State, _ map[string]any) (map[string]any, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", st.Nebula.Name)
	if st.Nebula.Source != "" {
		fmt.Fprintf(&b, "_Source: %s_\n\n", st.Nebula.Source)
	}
	if ctx := strings.TrimSpace(st.Nebula.Context); ctx != "" {
		b.WriteString("## Context\n\n")
		b.WriteString(ctx)
		b.WriteString("\n\n")
	}
	if len(st.Nebula.Phases) > 0 {
		b.WriteString("## Existing phases\n\n")
		for _, p := range st.Nebula.Phases {
			fmt.Fprintf(&b, "- [%s] %s (%s)\n", p.ID, p.Title, p.Status)
		}
	}
	if rt != nil {
		b.WriteString(renderPreCommitSection(rt.preCommit))
	}
	return map[string]any{"prompt": b.String()}, nil
}

// opRenderFixPrompt renders a fix brief for the architect from master-review's
// fix feedback (args["fix_feedback"]) plus the nebula's original context, so the
// architect plans corrective phases. Like render_seed it appends the repo's
// pre-commit commands so fix phases anticipate the same quality bar. Output:
// {"prompt": <markdown>}.
func opRenderFixPrompt(_ context.Context, rt *Runtime, st *State, args map[string]any) (map[string]any, error) {
	feedback, _ := args["fix_feedback"].(string)
	var b strings.Builder
	fmt.Fprintf(&b, "# Fix: %s\n\n", st.Nebula.Name)
	if fb := strings.TrimSpace(feedback); fb != "" {
		b.WriteString("## Master review feedback\n\n")
		b.WriteString(fb)
		b.WriteString("\n\n")
	}
	if ctx := strings.TrimSpace(st.Nebula.Context); ctx != "" {
		b.WriteString("## Original context\n\n")
		b.WriteString(ctx)
		b.WriteString("\n\n")
	}
	if rt != nil {
		b.WriteString(renderPreCommitSection(rt.preCommit))
	}
	return map[string]any{"prompt": b.String()}, nil
}

// renderPreCommitSection formats the repo's pre-commit commands as a Markdown
// section for the architect prompt. It returns the empty string when no commands
// are configured, so an unconfigured repo produces no extra block at all.
//
// DESIGN NOTE — this supersedes the spec's `${pre_commit_commands}` placeholder.
// The original Phase 6 design put a literal `${pre_commit_commands}` token in the
// architect star's template and string-replaced it here. That couples this
// operator to one star's *system* prompt, which it cannot reach: the operator
// builds the architect's *user*-prompt brief (the "prompt" output), while the
// star's system prompt is loaded separately in dispatchStar. Appending the
// section to the brief delivers the same pre-commit context without the
// coupling, so the star template carries no placeholder. See the phase summary
// for the recorded acceptance-criterion deviation.
func renderPreCommitSection(pre gitops.PreCommitConfig) string {
	if len(pre.Commands) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## Repository Pre-Commit Checks\n\n")
	b.WriteString("This repository enforces the following checks before every commit. ")
	b.WriteString("Your plan must produce code that passes all of them:\n\n")
	for _, c := range pre.Commands {
		fmt.Fprintf(&b, "- `%s`\n", c)
	}
	return b.String()
}

// phaseSpec is the structure the architect emits (schema phases-v1) and
// persist_phases consumes.
type phaseSpec struct {
	Phases []struct {
		ID              string `json:"id"`
		Title           string `json:"title"`
		Body            string `json:"body"`
		FrontmatterTOML string `json:"frontmatter_toml"`
	} `json:"phases"`
}

// opPersistPhases consumes the architect's schema-validated JSON output (a
// {"phases":[…]} object passed as args["phases_json"]) and inserts each phase
// into the nebula. Output: {"count": <n>}. The structured-output contract
// (schema phases-v1, enforced by the invoker) guarantees a valid object, so this
// no longer parses free-text TOML or strips Markdown fences.
func opPersistPhases(ctx context.Context, rt *Runtime, st *State, args map[string]any) (map[string]any, error) {
	raw, ok := args["phases_json"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("persist_phases: missing string input %q", "phases_json")
	}
	var spec phaseSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return nil, fmt.Errorf("persist_phases: parse phases: %w", err)
	}
	if len(spec.Phases) == 0 {
		return nil, fmt.Errorf("persist_phases: no phases in input")
	}
	for i, p := range spec.Phases {
		if p.ID == "" {
			return nil, fmt.Errorf("persist_phases: phase %d has empty id", i)
		}
		if err := rt.nebStore.InsertPhase(ctx, st.Nebula.ID, fabric.PhaseRow{
			ID:              p.ID,
			Seq:             i,
			Title:           p.Title,
			Body:            p.Body,
			FrontmatterTOML: p.FrontmatterTOML,
		}); err != nil {
			return nil, fmt.Errorf("persist_phases: insert %q: %w", p.ID, err)
		}
		rt.declarePhaseSymbols(ctx, p.ID, p.Body)
	}
	return map[string]any{"count": len(spec.Phases)}, nil
}

// declarePhaseSymbols seeds 'declared' entanglements for the producer symbols a
// phase's spec names in its "## Files" and "## Solution" sections. The run_id is
// left unset here — a later coder pre-flight binds it via Claim — so the symbol
// identity (producer + kind + name) carries the linkage. Best-effort and
// nil-safe: coordination is advisory, so a tracking failure is logged, never
// fatal to phase persistence.
func (r *Runtime) declarePhaseSymbols(ctx context.Context, phaseID, body string) {
	if r.entanglements == nil {
		return
	}
	for _, d := range neutron.ExtractDeclarations(body) {
		kind, ok := neutronKinds[d.Kind]
		if !ok {
			continue
		}
		err := r.entanglements.Declare(ctx, fabric.Entanglement{
			Producer: phaseID,
			PhaseID:  phaseID,
			Kind:     kind,
			Name:     d.Name,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "constellations: declare entanglement %q for phase %q: %v\n", d.Name, phaseID, err)
		}
	}
}

// applyTerminalEntanglements advances the run's entanglement lifecycle when the
// run reaches a terminal state: fulfilled on _done, withdrawn on _failed or
// _awaiting_human. Best-effort and nil-safe — a tracking failure is logged, not
// fatal, since coordination is advisory and must never block a run from
// terminating. (The cross-phase merge gate that gates Fulfill on a clean merge
// arrives with the supervisor in a later phase; terminating _done here fulfills
// the run's own in-flight symbols.)
func (r *Runtime) applyTerminalEntanglements(ctx context.Context, runID, state string) {
	if r.entanglements == nil {
		return
	}
	var err error
	switch state {
	case StateDone:
		err = r.entanglements.Fulfill(ctx, runID)
	case StateFailed, StateAwaitingHuman:
		err = r.entanglements.Withdraw(ctx, runID)
	default:
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "constellations: entanglement lifecycle (run %s → %s): %v\n", runID, state, err)
	}
}

// markInFlightFromCommit advances the lifecycle from the green-build commit
// node, reusing the commit diff (HEAD~1..HEAD) for two transitions:
//   - every top-level symbol the commit added or changed is marked in_flight
//     under run, recording the declaration text as the signature siblings read
//     in their pre-flight notes; and
//   - every top-level symbol the commit deleted is deprecated, so a downstream
//     consumer's pre-flight warns it not to reintroduce a use of the removed
//     symbol (the exact post-merge build failure this lifecycle exists to avoid).
//
// Best-effort and nil-safe — a diff or store failure is logged, never fatal,
// since coordination is advisory and must not break a run. A commit that wrote
// nothing (committed=false) is skipped.
func (r *Runtime) markInFlightFromCommit(ctx context.Context, run *fabric.RunRow, out map[string]any) {
	if r.entanglements == nil || r.committer == nil {
		return
	}
	if committed, _ := out["committed"].(bool); !committed {
		return
	}
	diff, err := r.committer.Diff(ctx, "HEAD~1", "HEAD")
	if err != nil {
		fmt.Fprintf(os.Stderr, "constellations: diff for in-flight marking (run %s): %v\n", run.ID, err)
		return
	}
	for _, t := range neutron.DetectTouchedSymbols(diff) {
		if err := r.entanglements.MarkInFlight(ctx, run.ID, t.Name, t.Signature); err != nil {
			fmt.Fprintf(os.Stderr, "constellations: mark in_flight %q (run %s): %v\n", t.Name, run.ID, err)
		}
	}
	for _, name := range neutron.DetectDeletions(diff) {
		if err := r.entanglements.Deprecate(ctx, run.ID, name); err != nil {
			fmt.Fprintf(os.Stderr, "constellations: deprecate %q (run %s): %v\n", name, run.ID, err)
		}
	}
}

// opCommit commits the working tree through the runtime's git seam. The runtime
// always threads the repo's [pre_commit] config into the commit (the operator
// never passes it), so a `commit` node placed after a coder star runs the
// repo's quality gate uniformly. A pre-commit failure with fail_on_error=true
// surfaces here as an error; an empty index (nothing to commit) is a normal
// outcome. Output: {"sha": <hash>, "committed": bool}.
func opCommit(ctx context.Context, rt *Runtime, _ *State, args map[string]any) (map[string]any, error) {
	message, _ := args["message"].(string)
	if strings.TrimSpace(message) == "" {
		message = "quasar: automated change"
	}
	sha, err := rt.commitWork(ctx, message)
	if err != nil {
		return nil, err
	}
	return map[string]any{"sha": sha, "committed": sha != ""}, nil
}

// opNotifyHuman flips the nebula to awaiting_human so it surfaces in the TUI as
// an approval item. Output: {"notified": true}.
func opNotifyHuman(ctx context.Context, rt *Runtime, st *State, _ map[string]any) (map[string]any, error) {
	if err := rt.nebStore.SetStatus(ctx, st.Nebula.ID, StateAwaitingHuman); err != nil {
		return nil, fmt.Errorf("notify_human: set status: %w", err)
	}
	return map[string]any{"notified": true}, nil
}

// opFailRun terminates a run with a structured failure reason. It is the
// give-up node of a guarded loop (e.g. master-review once its cycle cap is
// exhausted): the node records reason/detail into State, and an unconditional
// edge to _failed marks the run failed so no downstream work (e.g. opening a
// PR) runs. detail is passed through unchanged so a node may emit either a plain
// string or a structured breakdown (the engine's budget-exhaustion path records
// the same map shape directly via failBudget). A missing detail yields nil.
// Output: {"reason": <string>, "detail": <any>}.
func opFailRun(_ context.Context, _ *Runtime, _ *State, args map[string]any) (map[string]any, error) {
	reason, _ := args["reason"].(string)
	if strings.TrimSpace(reason) == "" {
		reason = "run failed"
	}
	return map[string]any{"reason": reason, "detail": args["detail"]}, nil
}

// opVerify returns the verify_<kind> operator. It runs the command supplied via
// args["command"] in the repo working dir and reports a structured outcome.
// Output: {"passed": bool, "output": <combined>, "kind": <kind>}. A non-zero
// exit is a normal (non-error) outcome — the constellation gates on `passed`.
func opVerify(kind string) Operator {
	return func(ctx context.Context, rt *Runtime, _ *State, args map[string]any) (map[string]any, error) {
		command, ok := args["command"].(string)
		if !ok || strings.TrimSpace(command) == "" {
			return nil, fmt.Errorf("verify_%s: missing string input %q", kind, "command")
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		if rt.repoPath != "" {
			cmd.Dir = rt.repoPath
		}
		out, err := cmd.CombinedOutput()
		passed := err == nil
		result := map[string]any{
			"passed": passed,
			"output": string(out),
			"kind":   kind,
		}
		// An ExitError means the command ran and failed its checks — that is a
		// verification outcome, not a runtime error. Any other error (command
		// not found, context canceled) is a real failure. errors.As is used so
		// a wrapped ExitError is still recognized.
		if err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				return nil, fmt.Errorf("verify_%s: run command: %w", kind, err)
			}
		}
		return result, nil
	}
}
