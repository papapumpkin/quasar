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
