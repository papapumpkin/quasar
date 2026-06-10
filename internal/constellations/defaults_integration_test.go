package constellations

import (
	"context"
	"encoding/json"
	"io/fs"
	"path"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/gitops"
)

// embeddedResolver is a PathResolver that always falls through to the embedded
// defaults, so a test exercises the shipped artifact files rather than fixtures.
type embeddedResolver struct{}

func (embeddedResolver) RepoPath() string                  { return "" }
func (embeddedResolver) ConstellationPath(string) string   { return artifacts.EmbeddedPath }
func (embeddedResolver) StarPath(string) string            { return artifacts.EmbeddedPath }
func (embeddedResolver) SkillPath(string) string           { return artifacts.EmbeddedPath }
func (embeddedResolver) SensorPath(string) (string, error) { return artifacts.EmbeddedPath, nil }
func (embeddedResolver) AllSensorPaths() ([]string, error) { return nil, nil }

// embeddedConstellationNames lists the embedded default constellations by
// reading their file names out of the embedded FS, so the guards below cover
// every shipped default without a hand-maintained list.
func embeddedConstellationNames(t *testing.T) []string {
	t.Helper()
	const dir = "defaults/constellations"
	entries, err := fs.ReadDir(artifacts.DefaultsFS, dir)
	if err != nil {
		t.Fatalf("read embedded constellations: %v", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		names = append(names, strings.TrimSuffix(path.Base(e.Name()), ".toml"))
	}
	if len(names) == 0 {
		t.Fatal("no embedded constellations found")
	}
	return names
}

// deferredOps are operators referenced by forward-looking embedded defaults
// whose implementation is intentionally out of scope for this nebula. The forge
// write path (push a branch, open a PR) lands in a follow-up; until then
// open-pr.toml names these ops as a published authoring example. The guard below
// permits exactly these, so a genuine typo or missing render-style operator
// still fails while the documented gap stays green.
var deferredOps = map[string]string{
	"gitops_push": "forge write path is a follow-up nebula; open-pr.toml ships as a forward-looking default",
	"gh_open_pr":  "forge write path is a follow-up nebula; open-pr.toml ships as a forward-looking default",
}

// TestEmbeddedDefaultReferencesResolve walks every embedded default
// constellation and asserts that each node's cross-reference resolves: builtin
// ops exist in the operator registry (or are documented deferrals), star nodes
// name a loadable star, and constellation nodes name a loadable
// sub-constellation. This is the guard that turns "default ships but can't
// execute" (an unregistered op, a typo'd ref) from a runtime failure into a
// test failure.
func TestEmbeddedDefaultReferencesResolve(t *testing.T) {
	t.Parallel()
	loader := artifacts.New(embeddedResolver{})

	for _, name := range embeddedConstellationNames(t) {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			con, err := loader.LoadConstellation(name)
			if err != nil {
				t.Fatalf("LoadConstellation(%q): %v", name, err)
			}
			for _, n := range con.Nodes {
				switch n.Type {
				case artifacts.NodeBuiltin:
					if _, ok := lookupOperator(n.Op); ok {
						continue
					}
					if reason, deferred := deferredOps[n.Op]; deferred {
						t.Logf("node %q references deferred operator %q (%s)", n.ID, n.Op, reason)
						continue
					}
					t.Errorf("node %q references unregistered operator %q", n.ID, n.Op)
				case artifacts.NodeStar:
					if _, err := loader.LoadStar(n.Star); err != nil {
						t.Errorf("node %q references unloadable star %q: %v", n.ID, n.Star, err)
					}
				case artifacts.NodeConstellation:
					if _, err := loader.LoadConstellation(n.Ref); err != nil {
						t.Errorf("node %q references unloadable constellation %q: %v", n.ID, n.Ref, err)
					}
				}
			}
		})
	}
}

// TestArchitectConstellationWiresPreCommitAndPersists drives the real embedded
// architect constellation end-to-end through the runtime. It guards the seam
// the unit tests miss: that render_seed's output key, the plan node's input
// reference, and persist's input reference all agree, so the rendered brief
// (including the pre-commit bar) actually reaches the architect star and the
// returned phases are persisted.
func TestArchitectConstellationWiresPreCommitAndPersists(t *testing.T) {
	ctx := context.Background()
	loader := artifacts.New(embeddedResolver{})
	// The architect star declares output_schema = "phases-v1", so the runtime
	// requests structured output and persist consumes result_json. The fake
	// returns the schema-valid object the real invoker would have produced.
	inv := &fakeInvoker{result: agent.InvocationResult{
		StructuredOutput: json.RawMessage(`{"phases":[{"id":"p1","title":"Do the work","body":"implement it"}]}`),
	}}
	rt, nebID := newTestRuntime(t, loader, inv)
	rt.preCommit = gitops.PreCommitConfig{Commands: []string{"gofmt -l .", "go test ./..."}}

	runID, err := rt.Fire(ctx, "architect", nebID, "", 0)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	driveToTerminal(ctx, t, rt, runID)

	// The pre-commit bar rendered by render_seed must reach the architect star
	// via the plan node's prompt input.
	if !strings.Contains(inv.gotPrompt, "## Repository Pre-Commit Checks") {
		t.Errorf("architect prompt missing pre-commit section:\n%s", inv.gotPrompt)
	}
	if !strings.Contains(inv.gotPrompt, "- `gofmt -l .`") {
		t.Errorf("architect prompt missing pre-commit bullet:\n%s", inv.gotPrompt)
	}

	// persist must have written the phases the architect returned, proving the
	// plan.result -> persist.phases_toml wiring is intact.
	neb, err := rt.nebStore.Get(ctx, nebID)
	if err != nil {
		t.Fatalf("Get nebula: %v", err)
	}
	if len(neb.Phases) != 1 || neb.Phases[0].ID != "p1" {
		t.Fatalf("nebula phases = %+v, want one phase p1", neb.Phases)
	}
}

// TestOpRenderFixPrompt covers the fix-loop brief: master-review feedback is
// rendered into the architect's prompt alongside the pre-commit bar.
func TestOpRenderFixPrompt(t *testing.T) {
	rt := &Runtime{preCommit: gitops.PreCommitConfig{Commands: []string{"go vet ./..."}}}
	st := NewState(NebulaSnapshot{Name: "Fix truncate", Context: "original goal"}, 1)

	out, err := opRenderFixPrompt(context.Background(), rt, st, map[string]any{
		"fix_feedback": "the helper still drops multibyte runes",
	})
	if err != nil {
		t.Fatalf("opRenderFixPrompt: %v", err)
	}
	prompt, _ := out["prompt"].(string)
	for _, want := range []string{
		"Fix: Fix truncate",
		"drops multibyte runes",
		"original goal",
		"## Repository Pre-Commit Checks",
		"- `go vet ./...`",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("fix prompt missing %q:\n%s", want, prompt)
		}
	}
}

// driveToTerminal steps a run until it reaches a terminal state or a step cap,
// failing the test on a step error or a run that never terminates.
func driveToTerminal(ctx context.Context, t *testing.T, rt *Runtime, runID string) {
	t.Helper()
	for i := 0; i < 32; i++ {
		state, err := rt.Step(ctx, runID)
		if err != nil {
			t.Fatalf("Step %d: %v", i, err)
		}
		if isTerminalState(state) {
			return
		}
	}
	t.Fatal("run did not reach a terminal state within the step cap")
}
