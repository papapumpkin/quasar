package snapshot

import (
	"strings"
	"testing"
)

func TestContextBudget_Compose_ZeroDisables(t *testing.T) {
	t.Parallel()
	cb := &ContextBudget{MaxTokens: 0}
	got := cb.Compose("project", "fabric", "prior")
	if got != "" {
		t.Errorf("expected empty string when MaxTokens=0, got %q", got)
	}
}

func TestContextBudget_Compose_AllEmpty(t *testing.T) {
	t.Parallel()
	cb := &ContextBudget{MaxTokens: DefaultMaxContextTokens}
	got := cb.Compose("", "", "")
	if got != "" {
		t.Errorf("expected empty string when all layers empty, got %q", got)
	}
}

func TestContextBudget_Compose_SingleLayer(t *testing.T) {
	t.Parallel()

	t.Run("project only", func(t *testing.T) {
		t.Parallel()
		cb := &ContextBudget{MaxTokens: DefaultMaxContextTokens}
		got := cb.Compose("project context here", "", "")
		if got != "project context here" {
			t.Errorf("expected project only, got %q", got)
		}
	})

	t.Run("fabric only", func(t *testing.T) {
		t.Parallel()
		cb := &ContextBudget{MaxTokens: DefaultMaxContextTokens}
		got := cb.Compose("", "fabric state", "")
		if got != "fabric state" {
			t.Errorf("expected fabric only, got %q", got)
		}
	})

	t.Run("prior work only", func(t *testing.T) {
		t.Parallel()
		cb := &ContextBudget{MaxTokens: DefaultMaxContextTokens}
		got := cb.Compose("", "", "prior work notes")
		if got != "prior work notes" {
			t.Errorf("expected prior work only, got %q", got)
		}
	})
}

func TestContextBudget_Compose_AllLayersPresent(t *testing.T) {
	t.Parallel()
	cb := &ContextBudget{MaxTokens: DefaultMaxContextTokens}
	got := cb.Compose("PROJECT", "FABRIC", "PRIOR")

	if !strings.Contains(got, "PROJECT") {
		t.Error("expected project context in output")
	}
	if !strings.Contains(got, "FABRIC") {
		t.Error("expected fabric state in output")
	}
	if !strings.Contains(got, "PRIOR") {
		t.Error("expected prior work in output")
	}
	// Verify separator between layers.
	if !strings.Contains(got, "---") {
		t.Error("expected --- separator between layers")
	}
	// Verify ordering: project comes first.
	projectIdx := strings.Index(got, "PROJECT")
	fabricIdx := strings.Index(got, "FABRIC")
	priorIdx := strings.Index(got, "PRIOR")
	if projectIdx >= fabricIdx {
		t.Error("project should appear before fabric")
	}
	if fabricIdx >= priorIdx {
		t.Error("fabric should appear before prior work")
	}
}

func TestContextBudget_Compose_WithinBudget(t *testing.T) {
	t.Parallel()
	budget := 100
	cb := &ContextBudget{MaxTokens: budget}
	maxChars := budget * charsPerToken

	project := strings.Repeat("p", maxChars*2)
	fabric := strings.Repeat("f", maxChars*2)
	prior := strings.Repeat("w", maxChars*2)

	got := cb.Compose(project, fabric, prior)
	// The output should not exceed the budget (with some tolerance for separators).
	// Each separator is "\n\n---\n\n" = 7 chars. With 2 separators = 14 chars.
	if len(got) > maxChars+14 {
		t.Errorf("output length %d exceeds budget of %d chars (+ separators)", len(got), maxChars)
	}
}

func TestContextBudget_Compose_BudgetRollover(t *testing.T) {
	t.Parallel()

	// With a small budget and no project context, fabric should get project's allocation too.
	budget := 50 // 200 chars total
	cb := &ContextBudget{MaxTokens: budget}
	// 40% for project = 80 chars, 40% for fabric = 80 chars, 20% for prior = 40 chars.
	// With empty project, fabric gets 80+80=160 chars.

	fabric := strings.Repeat("F", 150)
	got := cb.Compose("", fabric, "")

	if !strings.Contains(got, "FFF") {
		t.Error("expected fabric content in output")
	}
	// Fabric should get more than its base 40% allocation (80 chars).
	if len(got) < 100 {
		t.Errorf("expected rollover to give fabric more chars, got length %d", len(got))
	}
}

func TestContextBudget_Compose_TruncationMarker(t *testing.T) {
	t.Parallel()
	// Tiny budget that forces truncation.
	cb := &ContextBudget{MaxTokens: 10} // 40 chars
	longContent := strings.Repeat("x", 100)

	got := cb.Compose(longContent, "", "")
	if !strings.Contains(got, "[truncated]") {
		t.Error("expected [truncated] marker when content exceeds budget")
	}
}

func TestContextBudget_Compose_ProjectPriority(t *testing.T) {
	t.Parallel()
	// Budget allows ~120 chars (30 tokens * 4). Project gets 40% = 48 chars.
	// If project is small, fabric gets the rollover.
	cb := &ContextBudget{MaxTokens: 30}

	project := "small project"         // 13 chars, well under 48
	fabric := strings.Repeat("F", 200) // Way over budget

	got := cb.Compose(project, fabric, "")
	if !strings.Contains(got, "small project") {
		t.Error("project content should be fully preserved (highest priority)")
	}
}

func TestTruncateLayer(t *testing.T) {
	t.Parallel()

	t.Run("no truncation needed", func(t *testing.T) {
		t.Parallel()
		got := truncateLayer("hello world", 100)
		if got != "hello world" {
			t.Errorf("expected no truncation, got %q", got)
		}
	})

	t.Run("truncation at newline boundary", func(t *testing.T) {
		t.Parallel()
		content := "line one\nline two\nline three\nline four"
		got := truncateLayer(content, 30)
		if !strings.HasSuffix(got, "[truncated]") {
			t.Error("expected [truncated] suffix")
		}
		if strings.Contains(got, "line four") {
			t.Error("line four should have been truncated")
		}
	})

	t.Run("very small budget returns empty", func(t *testing.T) {
		t.Parallel()
		// Budget smaller than the truncation marker — content that exceeds it
		// should produce an empty string since the marker itself wouldn't fit.
		got := truncateLayer("hello world this is long", 5)
		if got != "" {
			t.Errorf("expected empty string for tiny budget, got %q", got)
		}
	})
}

func TestTruncateFabric_PreservesDiscoveries(t *testing.T) {
	t.Parallel()

	fabric := "## Entanglements\n" +
		"- ent1\n- ent2\n- ent3\n- ent4\n- ent5\n- ent6\n- ent7\n- ent8\n- ent9\n- ent10\n" +
		"\n## Discoveries\n" +
		"- BLOCKER: critical issue\n" +
		"\n## File Claims\n" +
		"- file1\n- file2\n"

	// Budget allows partial content.
	got := truncateFabric(fabric, 200)
	if !strings.Contains(got, "Discoveries") {
		t.Error("expected Discoveries header to be preserved")
	}
	if !strings.Contains(got, "BLOCKER") {
		t.Error("expected discovery content to be preserved")
	}
}

func TestTruncateFabric_NoTruncationNeeded(t *testing.T) {
	t.Parallel()
	content := "## Section\nsmall content"
	got := truncateFabric(content, 1000)
	if got != content {
		t.Errorf("expected no truncation, got %q", got)
	}
}

func TestTruncateFabric_FallbackForNonSectioned(t *testing.T) {
	t.Parallel()
	content := "no headers here, just plain text that is long enough to need truncation and goes on"
	got := truncateFabric(content, 40)
	if len(got) > 40 {
		t.Errorf("expected truncated output <= 40 chars, got %d", len(got))
	}
}

func TestParseFabricSections(t *testing.T) {
	t.Parallel()
	input := "## Entanglements\n- ent1\n- ent2\n\n### Discoveries\n- disc1\n\n## Claims\n- claim1\n"
	sections := parseFabricSections(input)

	if len(sections) != 3 {
		t.Fatalf("expected 3 sections, got %d", len(sections))
	}

	if sections[0].header != "## Entanglements" {
		t.Errorf("expected first header to be ## Entanglements, got %q", sections[0].header)
	}
	if sections[0].isDiscovery {
		t.Error("entanglements section should not be marked as discovery")
	}

	if sections[1].header != "### Discoveries" {
		t.Errorf("expected second header to be ### Discoveries, got %q", sections[1].header)
	}
	if !sections[1].isDiscovery {
		t.Error("discoveries section should be marked as discovery")
	}

	if sections[2].header != "## Claims" {
		t.Errorf("expected third header to be ## Claims, got %q", sections[2].header)
	}
}

func TestEstimateTokens(t *testing.T) {
	t.Parallel()

	t.Run("empty string", func(t *testing.T) {
		t.Parallel()
		if got := EstimateTokens(""); got != 0 {
			t.Errorf("expected 0 tokens for empty string, got %d", got)
		}
	})

	t.Run("exact multiple", func(t *testing.T) {
		t.Parallel()
		// 8 chars / 4 = 2 tokens
		if got := EstimateTokens("12345678"); got != 2 {
			t.Errorf("expected 2 tokens, got %d", got)
		}
	})

	t.Run("rounds up", func(t *testing.T) {
		t.Parallel()
		// 5 chars / 4 = 1.25, rounds up to 2
		if got := EstimateTokens("12345"); got != 2 {
			t.Errorf("expected 2 tokens (rounded up), got %d", got)
		}
	})
}

func TestContextBudget_DefaultTokens(t *testing.T) {
	t.Parallel()
	if DefaultMaxContextTokens != 10000 {
		t.Errorf("expected default of 10000, got %d", DefaultMaxContextTokens)
	}
}
