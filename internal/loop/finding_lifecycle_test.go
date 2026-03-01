package loop

import "testing"

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
