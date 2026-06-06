package constellations

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/gitops"
)

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

// phaseSpec is the structure the architect emits and persist_phases consumes.
type phaseSpec struct {
	Phases []struct {
		ID              string `toml:"id"`
		Title           string `toml:"title"`
		Body            string `toml:"body"`
		FrontmatterTOML string `toml:"frontmatter_toml"`
	} `toml:"phases"`
}

// opPersistPhases parses an architect's structured output (a TOML document with
// a [[phases]] array, passed as args["phases_toml"]) and inserts each phase
// into the nebula. Output: {"count": <n>}.
func opPersistPhases(ctx context.Context, rt *Runtime, st *State, args map[string]any) (map[string]any, error) {
	raw, ok := args["phases_toml"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("persist_phases: missing string input %q", "phases_toml")
	}
	var spec phaseSpec
	if err := toml.Unmarshal([]byte(raw), &spec); err != nil {
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
	}
	return map[string]any{"count": len(spec.Phases)}, nil
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
