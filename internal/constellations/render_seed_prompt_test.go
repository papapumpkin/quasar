package constellations

import (
	"context"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/gitops"
)

// TestRenderSeedPromptPreCommit covers the pre-commit interpolation path of the
// render_seed_prompt operator: a configured [pre_commit] block is rendered into
// the architect's brief as a bulleted checklist, and an unconfigured repo
// produces no extra block at all.
func TestRenderSeedPromptPreCommit(t *testing.T) {
	const heading = "## Repository Pre-Commit Checks"

	seed := NebulaSnapshot{
		Name:    "Fix truncate",
		Source:  "github",
		Context: "the truncate helper drops multibyte runes",
	}

	t.Run("renders configured commands as a checklist", func(t *testing.T) {
		commands := []string{"gofmt -l .", "go vet ./...", "go test ./..."}
		rt := &Runtime{preCommit: gitops.PreCommitConfig{Commands: commands, FailOnError: true}}
		st := NewState(seed, 1)

		out, err := opRenderSeedPrompt(context.Background(), rt, st, nil)
		if err != nil {
			t.Fatalf("opRenderSeedPrompt: %v", err)
		}
		prompt, _ := out["prompt"].(string)

		if !strings.Contains(prompt, heading) {
			t.Errorf("prompt missing pre-commit heading %q:\n%s", heading, prompt)
		}
		for _, c := range commands {
			if want := "- `" + c + "`"; !strings.Contains(prompt, want) {
				t.Errorf("prompt missing bullet %q:\n%s", want, prompt)
			}
		}
		// The seed context must survive alongside the appended section.
		if !strings.Contains(prompt, "multibyte runes") {
			t.Errorf("prompt dropped seed context:\n%s", prompt)
		}
	})

	t.Run("omits the block when no commands are configured", func(t *testing.T) {
		rt := &Runtime{preCommit: gitops.PreCommitConfig{}}
		st := NewState(seed, 1)

		out, err := opRenderSeedPrompt(context.Background(), rt, st, nil)
		if err != nil {
			t.Fatalf("opRenderSeedPrompt: %v", err)
		}
		prompt, _ := out["prompt"].(string)

		if strings.Contains(prompt, heading) {
			t.Errorf("unconfigured repo should emit no pre-commit block:\n%s", prompt)
		}
	})

	t.Run("nil runtime is tolerated", func(t *testing.T) {
		st := NewState(seed, 1)
		out, err := opRenderSeedPrompt(context.Background(), nil, st, nil)
		if err != nil {
			t.Fatalf("opRenderSeedPrompt: %v", err)
		}
		if prompt, _ := out["prompt"].(string); strings.Contains(prompt, heading) {
			t.Errorf("nil runtime should emit no pre-commit block:\n%s", prompt)
		}
	})
}
