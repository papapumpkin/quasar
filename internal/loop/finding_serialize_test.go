package loop

import (
	"strings"
	"testing"
)

func TestSerializeFindings(t *testing.T) {
	t.Parallel()

	t.Run("EmptySlice", func(t *testing.T) {
		t.Parallel()
		result := SerializeFindings(nil, 200)
		if result != "" {
			t.Errorf("expected empty string for nil findings, got %q", result)
		}
	})

	t.Run("SingleFinding", func(t *testing.T) {
		t.Parallel()
		findings := []ReviewFinding{{
			ID:          "f-abc123",
			Severity:    "critical",
			Description: "SQL injection vulnerability",
			Cycle:       1,
			Status:      FindingStatusFound,
		}}
		result := SerializeFindings(findings, 200)

		checks := []string{
			"1. [critical]",
			"id=f-abc123",
			"cycle=1",
			"status=found",
			"SQL injection vulnerability",
		}
		for _, want := range checks {
			if !strings.Contains(result, want) {
				t.Errorf("expected output to contain %q, got:\n%s", want, result)
			}
		}
	})

	t.Run("MultipleFindings", func(t *testing.T) {
		t.Parallel()
		findings := []ReviewFinding{
			{
				ID:          "f-aaa111",
				Severity:    "critical",
				Description: "null pointer dereference",
				Cycle:       1,
				Status:      FindingStatusStillPresent,
			},
			{
				ID:          "f-bbb222",
				Severity:    "minor",
				Description: "unused import",
				Cycle:       2,
				Status:      FindingStatusFixed,
			},
		}
		result := SerializeFindings(findings, 200)

		if !strings.Contains(result, "1. [critical]") {
			t.Error("expected first finding to be numbered 1")
		}
		if !strings.Contains(result, "2. [minor]") {
			t.Error("expected second finding to be numbered 2")
		}
		if !strings.Contains(result, "status=still_present") {
			t.Error("expected still_present status")
		}
		if !strings.Contains(result, "status=fixed") {
			t.Error("expected fixed status")
		}
	})

	t.Run("TruncatesLongDescription", func(t *testing.T) {
		t.Parallel()
		longDesc := strings.Repeat("x", 300)
		findings := []ReviewFinding{{
			ID:          "f-ccc333",
			Severity:    "major",
			Description: longDesc,
			Cycle:       1,
			Status:      FindingStatusFound,
		}}
		result := SerializeFindings(findings, 50)

		if !strings.Contains(result, "... [truncated]") {
			t.Errorf("expected truncation marker, got:\n%s", result)
		}
		// The serialized line should not contain the full 300-char description.
		if strings.Contains(result, longDesc) {
			t.Error("expected description to be truncated")
		}
	})

	t.Run("RegressedStatus", func(t *testing.T) {
		t.Parallel()
		findings := []ReviewFinding{{
			ID:          "f-ddd444",
			Severity:    "critical",
			Description: "race condition",
			Cycle:       1,
			Status:      FindingStatusRegressed,
		}}
		result := SerializeFindings(findings, 200)
		if !strings.Contains(result, "status=regressed") {
			t.Errorf("expected regressed status, got:\n%s", result)
		}
	})
}
