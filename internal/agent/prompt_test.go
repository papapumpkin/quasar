package agent

import (
	"strings"
	"testing"
)

func TestBuildSystemPrompt(t *testing.T) {
	t.Parallel()

	base := "You are a coder."

	t.Run("fabric disabled returns base only", func(t *testing.T) {
		t.Parallel()
		got := BuildSystemPrompt(base, PromptOpts{FabricEnabled: false})
		if got != base {
			t.Errorf("expected base prompt unchanged, got:\n%s", got)
		}
	})

	t.Run("fabric enabled appends protocol", func(t *testing.T) {
		t.Parallel()
		got := BuildSystemPrompt(base, PromptOpts{FabricEnabled: true})
		if !strings.HasPrefix(got, base) {
			t.Errorf("expected prompt to start with base, got:\n%s", got)
		}
		if !strings.Contains(got, "## Fabric Protocol") {
			t.Error("expected fabric protocol header in prompt")
		}
		if !strings.Contains(got, "quasar fabric entanglements") {
			t.Error("expected entanglements command in protocol")
		}
		if !strings.Contains(got, "quasar fabric claim") {
			t.Error("expected claim command in protocol")
		}
		if !strings.Contains(got, "quasar discovery") {
			t.Error("expected discovery command in protocol")
		}
		if !strings.Contains(got, "quasar pulse emit") {
			t.Error("expected pulse emit command in protocol")
		}
	})

	t.Run("fabric disabled does not contain protocol", func(t *testing.T) {
		t.Parallel()
		got := BuildSystemPrompt(base, PromptOpts{FabricEnabled: false})
		if strings.Contains(got, "Fabric Protocol") {
			t.Error("fabric protocol should not be present when disabled")
		}
	})

	t.Run("zero opts preserves backward compatibility", func(t *testing.T) {
		t.Parallel()
		got := BuildSystemPrompt(base, PromptOpts{})
		if got != base {
			t.Errorf("zero PromptOpts should return base unchanged, got:\n%s", got)
		}
	})

	t.Run("task ID is stored in opts", func(t *testing.T) {
		t.Parallel()
		opts := PromptOpts{FabricEnabled: true, TaskID: "phase-abc"}
		got := BuildSystemPrompt(base, opts)
		// TaskID is carried on opts for downstream use; BuildSystemPrompt
		// does not embed it directly but it should be available.
		if opts.TaskID != "phase-abc" {
			t.Errorf("expected TaskID to be preserved, got: %s", opts.TaskID)
		}
		if !strings.Contains(got, "## Fabric Protocol") {
			t.Error("expected fabric protocol in prompt")
		}
	})

	t.Run("project context prepended before base", func(t *testing.T) {
		t.Parallel()
		ctx := "# Project: quasar\nLanguage: Go"
		got := BuildSystemPrompt(base, PromptOpts{ProjectContext: ctx})
		if !strings.HasPrefix(got, ctx) {
			t.Errorf("expected prompt to start with project context, got:\n%s", got)
		}
		if !strings.Contains(got, "\n\n---\n\n") {
			t.Error("expected separator between project context and base prompt")
		}
		if !strings.Contains(got, base) {
			t.Error("expected base prompt to be present after project context")
		}
	})

	t.Run("project context ordering: context then base then fabric", func(t *testing.T) {
		t.Parallel()
		ctx := "# Project Snapshot"
		got := BuildSystemPrompt(base, PromptOpts{
			ProjectContext: ctx,
			FabricEnabled:  true,
		})
		ctxIdx := strings.Index(got, ctx)
		baseIdx := strings.Index(got, base)
		fabricIdx := strings.Index(got, "## Fabric Protocol")
		if ctxIdx < 0 || baseIdx < 0 || fabricIdx < 0 {
			t.Fatalf("missing expected section: ctx=%d base=%d fabric=%d", ctxIdx, baseIdx, fabricIdx)
		}
		if ctxIdx >= baseIdx {
			t.Errorf("project context (at %d) should appear before base prompt (at %d)", ctxIdx, baseIdx)
		}
		if baseIdx >= fabricIdx {
			t.Errorf("base prompt (at %d) should appear before fabric protocol (at %d)", baseIdx, fabricIdx)
		}
	})

	t.Run("empty project context produces same output as without it", func(t *testing.T) {
		t.Parallel()
		withCtx := BuildSystemPrompt(base, PromptOpts{ProjectContext: ""})
		without := BuildSystemPrompt(base, PromptOpts{})
		if withCtx != without {
			t.Errorf("empty ProjectContext should produce identical output:\nwith: %q\nwithout: %q", withCtx, without)
		}
	})
}

func TestFabricProtocolContent(t *testing.T) {
	t.Parallel()

	requiredPhrases := []string{
		"You are one of several concurrent coders",
		"quasar fabric entanglements",
		"quasar fabric claim --file",
		"quasar fabric post --from-file",
		"quasar discovery --kind file_conflict",
		"quasar discovery --kind entanglement_dispute",
		"quasar discovery --kind requirements_ambiguity",
		"quasar discovery --kind missing_dependency",
		"quasar pulse emit --kind decision",
		"quasar pulse emit --kind failure",
		"quasar pulse emit --kind note",
		"quasar pulse emit --kind reviewer_feedback",
		"Never modify files you haven't claimed",
		"Never change an entangled interface without posting a discovery",
	}

	for _, phrase := range requiredPhrases {
		t.Run(phrase, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(FabricProtocol, phrase) {
				t.Errorf("FabricProtocol missing required phrase: %q", phrase)
			}
		})
	}
}

func TestAdviceForStatus(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"declared":   "A sibling phase intends to produce this symbol. Avoid the name collision.",
		"claimed":    "A sibling coder has picked up work on this symbol. Coordinate or wait.",
		"in_flight":  "Use the current signature shown above.",
		"deprecated": "Do not introduce new uses. Use the replacement noted in the producer's spec.",
		"unknown":    "A sibling phase is working on this symbol. Treat it as a constraint.",
	}
	for status, want := range cases {
		status, want := status, want
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			if got := AdviceForStatus(status); got != want {
				t.Errorf("AdviceForStatus(%q) = %q, want %q", status, got, want)
			}
		})
	}
}

func TestAppendCoordinationNotes(t *testing.T) {
	t.Parallel()

	t.Run("empty notes leave prompt unchanged", func(t *testing.T) {
		t.Parallel()
		const prompt = "# Nebula: demo\n"
		if got := AppendCoordinationNotes(prompt, nil); got != prompt {
			t.Errorf("expected prompt unchanged with no notes, got:\n%q", got)
		}
	})

	t.Run("golden block for each status", func(t *testing.T) {
		t.Parallel()
		notes := []CoordinationNote{
			{
				Name:             "Sensor.Poll",
				Status:           "in_flight",
				SiblingPhaseID:   "rename-integrations-to-sensors",
				CurrentSignature: "Poll(ctx, cursor) ([]Event, json.RawMessage, error)",
			},
			{
				Name:           "FromTicket",
				Status:         "deprecated",
				SiblingPhaseID: "rename-integrations-to-sensors",
			},
			{
				Name:           "Scheduler",
				Status:         "declared",
				SiblingPhaseID: "github-sensor-produces-nebula",
			},
			{
				Name:           "Cursor",
				Status:         "claimed",
				SiblingPhaseID: "github-sensor-produces-nebula",
			},
		}
		const base = "# Nebula: demo"
		got := AppendCoordinationNotes(base, notes)

		const want = "# Nebula: demo\n" +
			"\n" +
			"## Coordination notes\n" +
			"Other phases are currently in flight on symbols that overlap your scope.\n" +
			"Both their work and yours are valid — treat these as constraints, not\n" +
			"optional guidance.\n" +
			"\n" +
			"- **Sensor.Poll** (in_flight, phase `rename-integrations-to-sensors`)\n" +
			"  Current signature: `Poll(ctx, cursor) ([]Event, json.RawMessage, error)`\n" +
			"  Advice: Use the current signature shown above.\n" +
			"\n" +
			"- **FromTicket** (deprecated, phase `rename-integrations-to-sensors`)\n" +
			"  Advice: Do not introduce new uses. Use the replacement noted in the producer's spec.\n" +
			"\n" +
			"- **Scheduler** (declared, phase `github-sensor-produces-nebula`)\n" +
			"  Advice: A sibling phase intends to produce this symbol. Avoid the name collision.\n" +
			"\n" +
			"- **Cursor** (claimed, phase `github-sensor-produces-nebula`)\n" +
			"  Advice: A sibling coder has picked up work on this symbol. Coordinate or wait.\n"

		if got != want {
			t.Errorf("rendered block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	t.Run("explicit advice overrides status-derived text", func(t *testing.T) {
		t.Parallel()
		notes := []CoordinationNote{{
			Name:           "FromTicket",
			Status:         "deprecated",
			SiblingPhaseID: "p1",
			Advice:         "Custom: use FromNebula instead.",
		}}
		got := AppendCoordinationNotes("base", notes)
		if !strings.Contains(got, "Advice: Custom: use FromNebula instead.") {
			t.Errorf("expected explicit advice to be rendered, got:\n%s", got)
		}
	})

	t.Run("deterministic output", func(t *testing.T) {
		t.Parallel()
		notes := []CoordinationNote{{Name: "Foo", Status: "in_flight", SiblingPhaseID: "p"}}
		first := AppendCoordinationNotes("base", notes)
		second := AppendCoordinationNotes("base", notes)
		if first != second {
			t.Error("AppendCoordinationNotes must be deterministic for prompt-cache stability")
		}
	})

	t.Run("prompt without trailing newline gets a clean separator", func(t *testing.T) {
		t.Parallel()
		notes := []CoordinationNote{{Name: "Foo", Status: "declared", SiblingPhaseID: "p"}}
		got := AppendCoordinationNotes("no newline", notes)
		if !strings.Contains(got, "no newline\n\n## Coordination notes") {
			t.Errorf("expected single blank line between prompt and block, got:\n%q", got)
		}
	})
}

func TestBuildSystemPromptDeterministic(t *testing.T) {
	t.Parallel()

	// BuildSystemPrompt must produce byte-identical output when called
	// with the same inputs. This is critical for prompt cache stability.
	opts := PromptOpts{
		FabricEnabled:  true,
		TaskID:         "phase-42",
		ProjectContext: "# Project: quasar\nLanguage: Go\nVersion: 1.25",
	}
	base := "You are a senior software engineer working as the CODER."

	first := BuildSystemPrompt(base, opts)
	second := BuildSystemPrompt(base, opts)

	if first != second {
		t.Errorf("BuildSystemPrompt is not deterministic:\nfirst len=%d\nsecond len=%d",
			len(first), len(second))
	}

	// Verify byte-level equality, not just string equality.
	if len(first) != len(second) {
		t.Fatalf("length mismatch: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("byte mismatch at offset %d: %q vs %q", i, first[i], second[i])
		}
	}
}
