package constellations

import (
	"context"
	"strings"
	"testing"
)

func TestOpRenderSeedPrompt(t *testing.T) {
	st := NewState(NebulaSnapshot{
		Name:    "Fix truncate",
		Source:  "github",
		Context: "the truncate helper drops multibyte runes",
		Phases:  []PhaseSnapshot{{ID: "p1", Title: "Add tests", Status: "pending"}},
	}, 1)
	out, err := opRenderSeedPrompt(context.Background(), nil, st, nil)
	if err != nil {
		t.Fatalf("opRenderSeedPrompt: %v", err)
	}
	prompt, _ := out["prompt"].(string)
	for _, want := range []string{"Fix truncate", "github", "multibyte runes", "Add tests"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestOpPersistPhases(t *testing.T) {
	ctx := context.Background()
	rt, nebID := newTestRuntime(t, &fakeLoader{}, nil)
	st := NewState(NebulaSnapshot{ID: nebID}, 1)

	t.Run("inserts phases", func(t *testing.T) {
		args := map[string]any{"phases_toml": `
[[phases]]
id = "p1"
title = "First"
body = "do first"

[[phases]]
id = "p2"
title = "Second"
body = "do second"
`}
		out, err := opPersistPhases(ctx, rt, st, args)
		if err != nil {
			t.Fatalf("opPersistPhases: %v", err)
		}
		if out["count"] != 2 {
			t.Fatalf("count = %v, want 2", out["count"])
		}
		neb, err := rt.nebStore.Get(ctx, nebID)
		if err != nil {
			t.Fatalf("Get nebula: %v", err)
		}
		if len(neb.Phases) != 2 {
			t.Fatalf("nebula has %d phases, want 2", len(neb.Phases))
		}
	})

	t.Run("missing input errors", func(t *testing.T) {
		if _, err := opPersistPhases(ctx, rt, st, map[string]any{}); err == nil {
			t.Fatal("expected error for missing phases_toml")
		}
	})
}

func TestOpVerify(t *testing.T) {
	ctx := context.Background()
	rt, _ := newTestRuntime(t, &fakeLoader{}, nil)
	verify := opVerify("test")

	t.Run("passing command", func(t *testing.T) {
		out, err := verify(ctx, rt, nil, map[string]any{"command": "exit 0"})
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if out["passed"] != true {
			t.Errorf("passed = %v, want true", out["passed"])
		}
	})

	t.Run("failing command is an outcome, not an error", func(t *testing.T) {
		out, err := verify(ctx, rt, nil, map[string]any{"command": "exit 1"})
		if err != nil {
			t.Fatalf("verify returned error for non-zero exit: %v", err)
		}
		if out["passed"] != false {
			t.Errorf("passed = %v, want false", out["passed"])
		}
	})

	t.Run("missing command errors", func(t *testing.T) {
		if _, err := verify(ctx, rt, nil, map[string]any{}); err == nil {
			t.Fatal("expected error for missing command")
		}
	})
}

func TestOperatorNamesRegistered(t *testing.T) {
	got := OperatorNames()
	want := []string{"commit", "fail_run", "master_review_decision", "notify_human", "persist_phases", "render_fix_prompt", "render_seed_prompt", "verify_build", "verify_lint", "verify_test"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("OperatorNames() = %v, want %v", got, want)
	}
}
