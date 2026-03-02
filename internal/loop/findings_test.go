package loop

import (
	"strings"
	"testing"
)

func TestFindingID(t *testing.T) {
	t.Parallel()

	t.Run("Deterministic", func(t *testing.T) {
		t.Parallel()
		id1 := FindingID("critical", "SQL injection vulnerability")
		id2 := FindingID("critical", "SQL injection vulnerability")
		if id1 != id2 {
			t.Errorf("same inputs produced different IDs: %q vs %q", id1, id2)
		}
	})

	t.Run("StableWithWhitespace", func(t *testing.T) {
		t.Parallel()
		id1 := FindingID("major", "missing error check")
		id2 := FindingID("major", "  missing error check  ")
		if id1 != id2 {
			t.Errorf("trimmed inputs should produce same ID: %q vs %q", id1, id2)
		}
	})

	t.Run("DifferentSeverity", func(t *testing.T) {
		t.Parallel()
		id1 := FindingID("critical", "missing error check")
		id2 := FindingID("minor", "missing error check")
		if id1 == id2 {
			t.Errorf("different severities should produce different IDs, both got %q", id1)
		}
	})

	t.Run("DifferentDescription", func(t *testing.T) {
		t.Parallel()
		id1 := FindingID("major", "missing error check")
		id2 := FindingID("major", "unused variable")
		if id1 == id2 {
			t.Errorf("different descriptions should produce different IDs, both got %q", id1)
		}
	})

	t.Run("HasPrefix", func(t *testing.T) {
		t.Parallel()
		id := FindingID("major", "some finding")
		if len(id) < 3 || id[:2] != "f-" {
			t.Errorf("expected ID to start with 'f-', got %q", id)
		}
	})

	t.Run("ConsistentLength", func(t *testing.T) {
		t.Parallel()
		id1 := FindingID("critical", "short")
		id2 := FindingID("minor", "a much longer description that goes on and on")
		if len(id1) != len(id2) {
			t.Errorf("IDs should have consistent length: %q (%d) vs %q (%d)",
				id1, len(id1), id2, len(id2))
		}
	})
}

func TestApplyVerifications(t *testing.T) {
	t.Parallel()

	t.Run("AllFixed", func(t *testing.T) {
		t.Parallel()
		findings := []ReviewFinding{
			{ID: "f-aaa", Severity: "critical", Description: "bug A", Cycle: 1, Status: FindingStatusFound},
			{ID: "f-bbb", Severity: "major", Description: "bug B", Cycle: 1, Status: FindingStatusFound},
		}
		verifications := []FindingVerification{
			{FindingID: "f-aaa", Status: FindingStatusFixed, Comment: "resolved"},
			{FindingID: "f-bbb", Status: FindingStatusFixed, Comment: "resolved"},
		}

		summary := ApplyVerifications(findings, verifications)

		if summary.Fixed != 2 {
			t.Errorf("expected Fixed=2, got %d", summary.Fixed)
		}
		if summary.StillPresent != 0 {
			t.Errorf("expected StillPresent=0, got %d", summary.StillPresent)
		}
		if summary.Regressed != 0 {
			t.Errorf("expected Regressed=0, got %d", summary.Regressed)
		}
		for _, f := range findings {
			if f.Status != FindingStatusFixed {
				t.Errorf("expected finding %s status=fixed, got %s", f.ID, f.Status)
			}
		}
	})

	t.Run("MixedStatuses", func(t *testing.T) {
		t.Parallel()
		findings := []ReviewFinding{
			{ID: "f-aaa", Severity: "critical", Description: "bug A", Cycle: 1, Status: FindingStatusFound},
			{ID: "f-bbb", Severity: "major", Description: "bug B", Cycle: 1, Status: FindingStatusFound},
			{ID: "f-ccc", Severity: "minor", Description: "bug C", Cycle: 1, Status: FindingStatusFound},
		}
		verifications := []FindingVerification{
			{FindingID: "f-aaa", Status: FindingStatusFixed},
			{FindingID: "f-bbb", Status: FindingStatusStillPresent},
			{FindingID: "f-ccc", Status: FindingStatusRegressed},
		}

		summary := ApplyVerifications(findings, verifications)

		if summary.Fixed != 1 {
			t.Errorf("expected Fixed=1, got %d", summary.Fixed)
		}
		if summary.StillPresent != 1 {
			t.Errorf("expected StillPresent=1, got %d", summary.StillPresent)
		}
		if summary.Regressed != 1 {
			t.Errorf("expected Regressed=1, got %d", summary.Regressed)
		}
		if findings[0].Status != FindingStatusFixed {
			t.Errorf("expected f-aaa status=fixed, got %s", findings[0].Status)
		}
		if findings[1].Status != FindingStatusStillPresent {
			t.Errorf("expected f-bbb status=still_present, got %s", findings[1].Status)
		}
		if findings[2].Status != FindingStatusRegressed {
			t.Errorf("expected f-ccc status=regressed, got %s", findings[2].Status)
		}
	})

	t.Run("UnknownFindingIDIgnored", func(t *testing.T) {
		t.Parallel()
		findings := []ReviewFinding{
			{ID: "f-aaa", Severity: "major", Description: "bug A", Cycle: 1, Status: FindingStatusFound},
		}
		verifications := []FindingVerification{
			{FindingID: "f-nonexistent", Status: FindingStatusFixed},
		}

		summary := ApplyVerifications(findings, verifications)

		if summary.Fixed != 0 || summary.StillPresent != 0 || summary.Regressed != 0 {
			t.Errorf("expected zero counts for unmatched ID, got %v", summary)
		}
		if findings[0].Status != FindingStatusFound {
			t.Errorf("expected unmatched finding to keep status=found, got %s", findings[0].Status)
		}
	})

	t.Run("EmptyVerifications", func(t *testing.T) {
		t.Parallel()
		findings := []ReviewFinding{
			{ID: "f-aaa", Severity: "major", Description: "bug A", Cycle: 1, Status: FindingStatusFound},
			{ID: "f-bbb", Severity: "minor", Description: "bug B", Cycle: 1, Status: FindingStatusFound},
		}

		summary := ApplyVerifications(findings, nil)

		if summary.Fixed != 0 || summary.StillPresent != 0 || summary.Regressed != 0 {
			t.Errorf("expected zero counts for empty verifications, got %v", summary)
		}
		for _, f := range findings {
			if f.Status != FindingStatusFound {
				t.Errorf("expected finding %s to retain status=found, got %s", f.ID, f.Status)
			}
		}
	})

	t.Run("EmptyFindings", func(t *testing.T) {
		t.Parallel()
		verifications := []FindingVerification{
			{FindingID: "f-aaa", Status: FindingStatusFixed},
		}

		summary := ApplyVerifications(nil, verifications)

		if summary.Fixed != 0 || summary.StillPresent != 0 || summary.Regressed != 0 {
			t.Errorf("expected zero counts for empty findings, got %v", summary)
		}
	})
}

func TestLifecycleSummaryHasUnresolved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		summary  LifecycleSummary
		expected bool
	}{
		{
			name:     "AllFixed",
			summary:  LifecycleSummary{Fixed: 3, StillPresent: 0, Regressed: 0},
			expected: false,
		},
		{
			name:     "ZeroCounts",
			summary:  LifecycleSummary{},
			expected: false,
		},
		{
			name:     "StillPresent",
			summary:  LifecycleSummary{Fixed: 1, StillPresent: 2, Regressed: 0},
			expected: true,
		},
		{
			name:     "Regressed",
			summary:  LifecycleSummary{Fixed: 1, StillPresent: 0, Regressed: 1},
			expected: true,
		},
		{
			name:     "BothUnresolved",
			summary:  LifecycleSummary{Fixed: 0, StillPresent: 1, Regressed: 1},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.summary.HasUnresolved()
			if got != tc.expected {
				t.Errorf("HasUnresolved() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestLifecycleSummaryString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		summary  LifecycleSummary
		expected string
	}{
		{
			name:     "AllZero",
			summary:  LifecycleSummary{},
			expected: "0 fixed, 0 still present, 0 regressed",
		},
		{
			name:     "MixedCounts",
			summary:  LifecycleSummary{Fixed: 2, StillPresent: 1, Regressed: 0},
			expected: "2 fixed, 1 still present, 0 regressed",
		},
		{
			name:     "AllPopulated",
			summary:  LifecycleSummary{Fixed: 3, StillPresent: 2, Regressed: 1},
			expected: "3 fixed, 2 still present, 1 regressed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.summary.String()
			if got != tc.expected {
				t.Errorf("String() = %q, want %q", got, tc.expected)
			}
		})
	}
}

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
