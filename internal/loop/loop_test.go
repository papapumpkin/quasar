package loop

import (
	"testing"
)

func TestParseReviewFindings_Approved(t *testing.T) {
	output := `The code looks good. All changes are correct and well-structured.

APPROVED: Changes implement the requested feature correctly with proper error handling.`

	findings := ParseReviewFindings(output)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for approved review, got %d", len(findings))
	}

	if !isApproved(output) {
		t.Error("expected isApproved to return true")
	}
}

func TestParseReviewFindings_SingleIssue(t *testing.T) {
	output := `I found a problem with the error handling.

ISSUE:
SEVERITY: critical
DESCRIPTION: The database connection is never closed, which will leak connections under load.`

	findings := ParseReviewFindings(output)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}

	if findings[0].Severity != "critical" {
		t.Errorf("expected severity 'critical', got %q", findings[0].Severity)
	}

	if findings[0].Description == "" {
		t.Error("expected non-empty description")
	}

	if isApproved(output) {
		t.Error("expected isApproved to return false")
	}
}

func TestParseReviewFindings_MultipleIssues(t *testing.T) {
	output := `Several issues found:

ISSUE:
SEVERITY: major
DESCRIPTION: Missing input validation on the user ID parameter.

ISSUE:
SEVERITY: minor
DESCRIPTION: Variable name 'x' is not descriptive enough.

ISSUE:
SEVERITY: critical
DESCRIPTION: SQL query is built with string concatenation, vulnerable to injection.`

	findings := ParseReviewFindings(output)
	if len(findings) != 3 {
		t.Fatalf("expected 3 findings, got %d", len(findings))
	}

	expected := []struct {
		severity string
	}{
		{"major"},
		{"minor"},
		{"critical"},
	}

	for i, e := range expected {
		if findings[i].Severity != e.severity {
			t.Errorf("finding %d: expected severity %q, got %q", i, e.severity, findings[i].Severity)
		}
	}
}

func TestParseReviewFindings_NoSeverity(t *testing.T) {
	output := `ISSUE:
DESCRIPTION: Missing error check on file open.`

	findings := ParseReviewFindings(output)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}

	if findings[0].Severity != "major" {
		t.Errorf("expected default severity 'major', got %q", findings[0].Severity)
	}
}

func TestParseReviewFindings_MultilineDescription(t *testing.T) {
	output := `ISSUE:
SEVERITY: major
DESCRIPTION: The retry logic has a bug where it retries on
non-retriable errors like 400 Bad Request, which will never succeed
and wastes API budget.`

	findings := ParseReviewFindings(output)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}

	if findings[0].Severity != "major" {
		t.Errorf("expected severity 'major', got %q", findings[0].Severity)
	}

	// Check that continuation lines are included.
	if !contains(findings[0].Description, "non-retriable") {
		t.Errorf("expected description to contain 'non-retriable', got %q", findings[0].Description)
	}
}

func TestIsApproved(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"APPROVED: Looks good", true},
		{"  APPROVED: All checks pass", true},
		{"Some preamble\n\nAPPROVED: Fine", true},
		{"ISSUE:\nSEVERITY: major\nDESCRIPTION: Bug", false},
		{"Not approved yet", false},
		{"", false},
	}

	for _, tt := range tests {
		result := isApproved(tt.input)
		if result != tt.expected {
			t.Errorf("isApproved(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
