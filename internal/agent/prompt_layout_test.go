package agent

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestPromptZoneClassification(t *testing.T) {
	t.Parallel()

	t.Run("system prompt contains only stable content", func(t *testing.T) {
		t.Parallel()

		projectCtx := "# Project: quasar\nLanguage: Go\nModule: github.com/aaronsalm/quasar"
		basePrompt := "You are a senior software engineer working as the CODER."
		sysPrompt := BuildSystemPrompt(basePrompt, PromptOpts{
			ProjectContext: projectCtx,
			FabricEnabled:  true,
		})

		// Verify all stable content is present.
		if !strings.Contains(sysPrompt, projectCtx) {
			t.Error("system prompt should contain project context")
		}
		if !strings.Contains(sysPrompt, basePrompt) {
			t.Error("system prompt should contain base prompt")
		}
		if !strings.Contains(sysPrompt, "## Fabric Protocol") {
			t.Error("system prompt should contain fabric protocol")
		}

		// Verify volatile content is absent from system prompt.
		volatileMarkers := []struct {
			label   string
			pattern string
		}{
			{"reviewer findings", "## Review Findings"},
			{"lint output", "## Lint Output"},
			{"filter output", "## Filter Output"},
			{"fabric snapshot", "## Fabric State"},
			{"refactor instructions", "## Refactor"},
			{"task description", "## Task"},
		}
		for _, vm := range volatileMarkers {
			if strings.Contains(sysPrompt, vm.pattern) {
				t.Errorf("system prompt should not contain volatile %s marker %q", vm.label, vm.pattern)
			}
		}
	})

	t.Run("ordering: project context before base before fabric", func(t *testing.T) {
		t.Parallel()

		projectCtx := "# Project Snapshot"
		basePrompt := "You are a coder agent."
		sysPrompt := BuildSystemPrompt(basePrompt, PromptOpts{
			ProjectContext: projectCtx,
			FabricEnabled:  true,
		})

		ctxIdx := strings.Index(sysPrompt, projectCtx)
		baseIdx := strings.Index(sysPrompt, basePrompt)
		fabricIdx := strings.Index(sysPrompt, "## Fabric Protocol")

		if ctxIdx < 0 || baseIdx < 0 || fabricIdx < 0 {
			t.Fatalf("missing section: ctx=%d base=%d fabric=%d", ctxIdx, baseIdx, fabricIdx)
		}
		if ctxIdx >= baseIdx {
			t.Errorf("project context (at %d) must appear before base prompt (at %d)", ctxIdx, baseIdx)
		}
		if baseIdx >= fabricIdx {
			t.Errorf("base prompt (at %d) must appear before fabric protocol (at %d)", baseIdx, fabricIdx)
		}
	})

	t.Run("system prompt is stable across repeated builds", func(t *testing.T) {
		t.Parallel()

		projectCtx := "# Project: quasar\nLanguage: Go"
		basePrompt := "You are the coder."
		opts := PromptOpts{
			ProjectContext: projectCtx,
			FabricEnabled:  true,
		}

		first := BuildSystemPrompt(basePrompt, opts)
		second := BuildSystemPrompt(basePrompt, opts)

		if first != second {
			t.Error("BuildSystemPrompt should produce byte-identical output for identical inputs")
		}
	})
}

func TestPromptZoneConstants(t *testing.T) {
	t.Parallel()

	t.Run("ZoneStablePrefix is zero value", func(t *testing.T) {
		t.Parallel()
		if ZoneStablePrefix != 0 {
			t.Errorf("ZoneStablePrefix should be 0 (iota), got %d", ZoneStablePrefix)
		}
	})

	t.Run("ZoneVolatileSuffix follows ZoneStablePrefix", func(t *testing.T) {
		t.Parallel()
		if ZoneVolatileSuffix != 1 {
			t.Errorf("ZoneVolatileSuffix should be 1, got %d", ZoneVolatileSuffix)
		}
	})

	t.Run("String returns expected names", func(t *testing.T) {
		t.Parallel()
		if got := ZoneStablePrefix.String(); got != "stable-prefix" {
			t.Errorf("ZoneStablePrefix.String() = %q, want %q", got, "stable-prefix")
		}
		if got := ZoneVolatileSuffix.String(); got != "volatile-suffix" {
			t.Errorf("ZoneVolatileSuffix.String() = %q, want %q", got, "volatile-suffix")
		}
	})

	t.Run("unknown zone has fallback string", func(t *testing.T) {
		t.Parallel()
		unknown := PromptZone(99)
		got := unknown.String()
		if !strings.Contains(got, "unknown") {
			t.Errorf("unknown zone should contain 'unknown', got %q", got)
		}
	})
}

func TestContentZoneClassification(t *testing.T) {
	t.Parallel()

	stableLabels := []string{
		LabelProjectContext,
		LabelBasePrompt,
		LabelFabricProtocol,
	}
	volatileLabels := []string{
		LabelTaskDescription,
		LabelReviewerFindings,
		LabelCoderOutput,
		LabelLintOutput,
		LabelFilterOutput,
		LabelFabricSnapshot,
		LabelRefactorInstructions,
	}

	t.Run("stable labels map to ZoneStablePrefix", func(t *testing.T) {
		t.Parallel()
		for _, label := range stableLabels {
			zone, ok := ContentZone[label]
			if !ok {
				t.Errorf("label %q not found in ContentZone", label)
				continue
			}
			if zone != ZoneStablePrefix {
				t.Errorf("label %q should be ZoneStablePrefix, got %v", label, zone)
			}
		}
	})

	t.Run("volatile labels map to ZoneVolatileSuffix", func(t *testing.T) {
		t.Parallel()
		for _, label := range volatileLabels {
			zone, ok := ContentZone[label]
			if !ok {
				t.Errorf("label %q not found in ContentZone", label)
				continue
			}
			if zone != ZoneVolatileSuffix {
				t.Errorf("label %q should be ZoneVolatileSuffix, got %v", label, zone)
			}
		}
	})

	t.Run("all labels accounted for", func(t *testing.T) {
		t.Parallel()
		expectedCount := len(stableLabels) + len(volatileLabels)
		if len(ContentZone) != expectedCount {
			t.Errorf("ContentZone has %d entries, expected %d", len(ContentZone), expectedCount)
		}
	})
}

func TestPromptManifest(t *testing.T) {
	t.Parallel()

	t.Run("NewPromptManifest computes SHA-256 hash", func(t *testing.T) {
		t.Parallel()
		sysPrompt := "stable system prompt content"
		m := NewPromptManifest(sysPrompt, 512)

		h := sha256.Sum256([]byte(sysPrompt))
		want := fmt.Sprintf("%x", h)

		if m.SystemPromptHash != want {
			t.Errorf("hash = %q, want %q", m.SystemPromptHash, want)
		}
	})

	t.Run("NewPromptManifest stores user prompt length", func(t *testing.T) {
		t.Parallel()
		m := NewPromptManifest("sys", 1024)
		if m.UserPromptLen != 1024 {
			t.Errorf("UserPromptLen = %d, want 1024", m.UserPromptLen)
		}
	})

	t.Run("NewPromptManifest copies canonical zone map", func(t *testing.T) {
		t.Parallel()
		m := NewPromptManifest("sys", 0)

		// Verify the zone map has all entries from ContentZone.
		if len(m.Zone) != len(ContentZone) {
			t.Errorf("zone map has %d entries, want %d", len(m.Zone), len(ContentZone))
		}
		for k, v := range ContentZone {
			got, ok := m.Zone[k]
			if !ok {
				t.Errorf("missing label %q in manifest zone map", k)
				continue
			}
			if got != v {
				t.Errorf("zone for %q = %v, want %v", k, got, v)
			}
		}

		// Verify it's a copy, not a reference to the original.
		m.Zone["test-label"] = ZoneStablePrefix
		if _, ok := ContentZone["test-label"]; ok {
			t.Error("modifying manifest zone map should not affect ContentZone")
		}
	})

	t.Run("identical system prompts produce identical hashes", func(t *testing.T) {
		t.Parallel()
		a := NewPromptManifest("same content", 100)
		b := NewPromptManifest("same content", 200)
		if a.SystemPromptHash != b.SystemPromptHash {
			t.Error("identical system prompts should produce identical hashes")
		}
	})

	t.Run("different system prompts produce different hashes", func(t *testing.T) {
		t.Parallel()
		a := NewPromptManifest("content A", 100)
		b := NewPromptManifest("content B", 100)
		if a.SystemPromptHash == b.SystemPromptHash {
			t.Error("different system prompts should produce different hashes")
		}
	})
}
