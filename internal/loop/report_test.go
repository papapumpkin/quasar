package loop

import (
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
)

func TestParseReviewReport(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		wantNil          bool
		wantSatisfaction string
		wantRisk         string
		wantHumanReview  bool
		wantSummary      bool // true if summary should be non-empty
	}{
		{
			name: "Full",
			input: `The code looks great.

APPROVED: Clean implementation with proper error handling.

REPORT:
SATISFACTION: high
RISK: low
NEEDS_HUMAN_REVIEW: no
SUMMARY: Clean implementation of rune-based truncation with proper edge case handling.`,
			wantSatisfaction: "high",
			wantRisk:         "low",
			wantHumanReview:  false,
			wantSummary:      true,
		},
		{
			name: "NeedsHumanReview",
			input: `ISSUE:
SEVERITY: major
DESCRIPTION: Security concern.

REPORT:
SATISFACTION: low
RISK: high
NEEDS_HUMAN_REVIEW: yes
SUMMARY: Significant security concerns that require human review.`,
			wantSatisfaction: "low",
			wantRisk:         "high",
			wantHumanReview:  true,
		},
		{
			name:    "Missing",
			input:   `APPROVED: Looks good, no issues.`,
			wantNil: true,
		},
		{
			name: "MediumValues",
			input: `REPORT:
SATISFACTION: medium
RISK: medium
NEEDS_HUMAN_REVIEW: no
SUMMARY: Acceptable implementation with minor style concerns.`,
			wantSatisfaction: "medium",
			wantRisk:         "medium",
			wantHumanReview:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			report := ParseReviewReport(tt.input)

			if tt.wantNil {
				if report != nil {
					t.Error("expected nil report when no REPORT: block present")
				}
				return
			}

			if report == nil {
				t.Fatal("expected non-nil report")
			}
			if report.Satisfaction != tt.wantSatisfaction {
				t.Errorf("expected satisfaction %q, got %q", tt.wantSatisfaction, report.Satisfaction)
			}
			if report.Risk != tt.wantRisk {
				t.Errorf("expected risk %q, got %q", tt.wantRisk, report.Risk)
			}
			if report.NeedsHumanReview != tt.wantHumanReview {
				t.Errorf("NeedsHumanReview = %v, want %v", report.NeedsHumanReview, tt.wantHumanReview)
			}
			if tt.wantSummary && report.Summary == "" {
				t.Error("expected non-empty summary")
			}
		})
	}
}

func TestFormatReportComment(t *testing.T) {
	r := &agent.ReviewReport{
		Satisfaction:     "high",
		Risk:             "low",
		NeedsHumanReview: false,
		Summary:          "All good.",
	}
	comment := FormatReportComment(r)
	if !strings.Contains(comment, "Satisfaction: high") {
		t.Errorf("expected satisfaction in comment, got %q", comment)
	}
	if !strings.Contains(comment, "Risk: low") {
		t.Errorf("expected risk in comment, got %q", comment)
	}
	if !strings.Contains(comment, "Needs human review: no") {
		t.Errorf("expected human review in comment, got %q", comment)
	}
}
