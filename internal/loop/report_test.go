package loop

import "testing"

func TestParseReviewReport_Full(t *testing.T) {
	output := `The code looks great.

APPROVED: Clean implementation with proper error handling.

REPORT:
SATISFACTION: high
RISK: low
NEEDS_HUMAN_REVIEW: no
SUMMARY: Clean implementation of rune-based truncation with proper edge case handling.`

	report := ParseReviewReport(output)
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.Satisfaction != "high" {
		t.Errorf("expected satisfaction 'high', got %q", report.Satisfaction)
	}
	if report.Risk != "low" {
		t.Errorf("expected risk 'low', got %q", report.Risk)
	}
	if report.NeedsHumanReview {
		t.Error("expected NeedsHumanReview to be false")
	}
	if report.Summary == "" {
		t.Error("expected non-empty summary")
	}
}

func TestParseReviewReport_NeedsHumanReview(t *testing.T) {
	output := `ISSUE:
SEVERITY: major
DESCRIPTION: Security concern.

REPORT:
SATISFACTION: low
RISK: high
NEEDS_HUMAN_REVIEW: yes
SUMMARY: Significant security concerns that require human review.`

	report := ParseReviewReport(output)
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.Satisfaction != "low" {
		t.Errorf("expected satisfaction 'low', got %q", report.Satisfaction)
	}
	if report.Risk != "high" {
		t.Errorf("expected risk 'high', got %q", report.Risk)
	}
	if !report.NeedsHumanReview {
		t.Error("expected NeedsHumanReview to be true")
	}
}

func TestParseReviewReport_Missing(t *testing.T) {
	output := `APPROVED: Looks good, no issues.`

	report := ParseReviewReport(output)
	if report != nil {
		t.Error("expected nil report when no REPORT: block present")
	}
}

func TestParseReviewReport_MediumValues(t *testing.T) {
	output := `REPORT:
SATISFACTION: medium
RISK: medium
NEEDS_HUMAN_REVIEW: no
SUMMARY: Acceptable implementation with minor style concerns.`

	report := ParseReviewReport(output)
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.Satisfaction != "medium" {
		t.Errorf("expected satisfaction 'medium', got %q", report.Satisfaction)
	}
	if report.Risk != "medium" {
		t.Errorf("expected risk 'medium', got %q", report.Risk)
	}
}

func TestFormatReportComment(t *testing.T) {
	r := &ReviewReport{
		Satisfaction:     "high",
		Risk:             "low",
		NeedsHumanReview: false,
		Summary:          "All good.",
	}
	comment := FormatReportComment(r)
	if !contains(comment, "Satisfaction: high") {
		t.Errorf("expected satisfaction in comment, got %q", comment)
	}
	if !contains(comment, "Risk: low") {
		t.Errorf("expected risk in comment, got %q", comment)
	}
	if !contains(comment, "Needs human review: no") {
		t.Errorf("expected human review in comment, got %q", comment)
	}
}
