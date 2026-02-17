package nebula

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarshalPhaseFile(t *testing.T) {
	t.Parallel()

	t.Run("basic spec", func(t *testing.T) {
		t.Parallel()
		spec := PhaseSpec{
			ID:        "rate-limiting",
			Title:     "Add rate limiting",
			Type:      "feature",
			Priority:  2,
			DependsOn: []string{"auth-middleware", "setup-models"},
			Body:      "Implement token bucket rate limiting.",
		}

		data, err := MarshalPhaseFile(spec)
		if err != nil {
			t.Fatalf("MarshalPhaseFile: %v", err)
		}

		content := string(data)
		if !strings.HasPrefix(content, "+++\n") {
			t.Error("output should start with +++ delimiter")
		}
		if !strings.Contains(content, "+++\n\nImplement token bucket") {
			t.Error("body should appear after closing delimiter")
		}
		if !strings.Contains(content, "id = ") || !strings.Contains(content, "rate-limiting") {
			t.Error("should contain id field")
		}
		if !strings.Contains(content, "title = ") || !strings.Contains(content, "Add rate limiting") {
			t.Error("should contain title field")
		}
		if !strings.Contains(content, "depends_on") {
			t.Error("should contain depends_on field")
		}
	})

	t.Run("omits zero-value optional fields", func(t *testing.T) {
		t.Parallel()
		spec := PhaseSpec{
			ID:    "minimal",
			Title: "Minimal phase",
		}

		data, err := MarshalPhaseFile(spec)
		if err != nil {
			t.Fatalf("MarshalPhaseFile: %v", err)
		}

		content := string(data)
		if strings.Contains(content, "priority") {
			t.Error("zero-value priority should be omitted")
		}
		if strings.Contains(content, "depends_on") {
			t.Error("nil depends_on should be omitted")
		}
		if strings.Contains(content, "labels") {
			t.Error("nil labels should be omitted")
		}
	})

	t.Run("empty body", func(t *testing.T) {
		t.Parallel()
		spec := PhaseSpec{
			ID:    "no-body",
			Title: "Phase without body",
		}

		data, err := MarshalPhaseFile(spec)
		if err != nil {
			t.Fatalf("MarshalPhaseFile: %v", err)
		}

		content := string(data)
		// Should end right after the closing +++
		if !strings.HasSuffix(content, "+++\n") {
			t.Errorf("empty body should end with closing delimiter, got: %q", content[len(content)-20:])
		}
	})

	t.Run("round-trip with parsePhaseFile", func(t *testing.T) {
		t.Parallel()
		original := PhaseSpec{
			ID:              "roundtrip-test",
			Title:           "Round-trip test phase",
			Type:            "task",
			Priority:        3,
			DependsOn:       []string{"dep-a", "dep-b"},
			Labels:          []string{"backend"},
			MaxReviewCycles: 5,
			MaxBudgetUSD:    10.5,
			Gate:            "manual",
			Body:            "## Description\n\nThis phase tests round-trip serialization.",
		}

		data, err := MarshalPhaseFile(original)
		if err != nil {
			t.Fatalf("MarshalPhaseFile: %v", err)
		}

		// Write to temp file and parse back.
		dir := t.TempDir()
		path := filepath.Join(dir, "roundtrip-test.md")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		parsed, err := parsePhaseFile(path, Defaults{})
		if err != nil {
			t.Fatalf("parsePhaseFile: %v", err)
		}

		if parsed.ID != original.ID {
			t.Errorf("ID: got %q, want %q", parsed.ID, original.ID)
		}
		if parsed.Title != original.Title {
			t.Errorf("Title: got %q, want %q", parsed.Title, original.Title)
		}
		if parsed.Type != original.Type {
			t.Errorf("Type: got %q, want %q", parsed.Type, original.Type)
		}
		if parsed.Priority != original.Priority {
			t.Errorf("Priority: got %d, want %d", parsed.Priority, original.Priority)
		}
		if len(parsed.DependsOn) != len(original.DependsOn) {
			t.Errorf("DependsOn len: got %d, want %d", len(parsed.DependsOn), len(original.DependsOn))
		}
		for i, dep := range original.DependsOn {
			if i < len(parsed.DependsOn) && parsed.DependsOn[i] != dep {
				t.Errorf("DependsOn[%d]: got %q, want %q", i, parsed.DependsOn[i], dep)
			}
		}
		if parsed.MaxReviewCycles != original.MaxReviewCycles {
			t.Errorf("MaxReviewCycles: got %d, want %d", parsed.MaxReviewCycles, original.MaxReviewCycles)
		}
		if parsed.MaxBudgetUSD != original.MaxBudgetUSD {
			t.Errorf("MaxBudgetUSD: got %f, want %f", parsed.MaxBudgetUSD, original.MaxBudgetUSD)
		}
		if parsed.Gate != original.Gate {
			t.Errorf("Gate: got %q, want %q", parsed.Gate, original.Gate)
		}
		if parsed.Body != strings.TrimSpace(original.Body) {
			t.Errorf("Body: got %q, want %q", parsed.Body, strings.TrimSpace(original.Body))
		}
	})
}
