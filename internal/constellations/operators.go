package constellations

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// opRenderSeedPrompt renders a seed nebula into a Markdown brief the architect
// star consumes. It reads the nebula snapshot already in State, so it makes no
// database call. Output: {"prompt": <markdown>}.
func opRenderSeedPrompt(_ context.Context, _ *Runtime, st *State, _ map[string]any) (map[string]any, error) {
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
	return map[string]any{"prompt": b.String()}, nil
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
		// not found, context canceled) is a real failure.
		if err != nil {
			if _, isExit := err.(*exec.ExitError); !isExit {
				return nil, fmt.Errorf("verify_%s: run command: %w", kind, err)
			}
		}
		return result, nil
	}
}
