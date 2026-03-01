package loop

import (
	"strings"
	"testing"
)

func TestParseReviewFindings(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		wantLen          int
		wantApproved     bool
		wantSeverities   []string
		wantDescContains string // substring check on first finding's description
	}{
		{
			name: "Approved",
			input: `The code looks good. All changes are correct and well-structured.

APPROVED: Changes implement the requested feature correctly with proper error handling.`,
			wantLen:      0,
			wantApproved: true,
		},
		{
			name: "SingleIssue",
			input: `I found a problem with the error handling.

ISSUE:
SEVERITY: critical
DESCRIPTION: The database connection is never closed, which will leak connections under load.`,
			wantLen:          1,
			wantApproved:     false,
			wantSeverities:   []string{"critical"},
			wantDescContains: "database connection",
		},
		{
			name: "MultipleIssues",
			input: `Several issues found:

ISSUE:
SEVERITY: major
DESCRIPTION: Missing input validation on the user ID parameter.

ISSUE:
SEVERITY: minor
DESCRIPTION: Variable name 'x' is not descriptive enough.

ISSUE:
SEVERITY: critical
DESCRIPTION: SQL query is built with string concatenation, vulnerable to injection.`,
			wantLen:        3,
			wantSeverities: []string{"major", "minor", "critical"},
		},
		{
			name: "NoSeverity",
			input: `ISSUE:
DESCRIPTION: Missing error check on file open.`,
			wantLen:        1,
			wantSeverities: []string{"major"},
		},
		{
			name: "MultilineDescription",
			input: `ISSUE:
SEVERITY: major
DESCRIPTION: The retry logic has a bug where it retries on
non-retriable errors like 400 Bad Request, which will never succeed
and wastes API budget.`,
			wantLen:          1,
			wantSeverities:   []string{"major"},
			wantDescContains: "non-retriable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			findings := ParseReviewFindings(tt.input)
			if len(findings) != tt.wantLen {
				t.Fatalf("expected %d findings, got %d", tt.wantLen, len(findings))
			}

			if tt.wantApproved != isApproved(tt.input) {
				t.Errorf("isApproved() = %v, want %v", !tt.wantApproved, tt.wantApproved)
			}

			for i, sev := range tt.wantSeverities {
				if findings[i].Severity != sev {
					t.Errorf("finding %d: expected severity %q, got %q", i, sev, findings[i].Severity)
				}
			}

			if tt.wantDescContains != "" && len(findings) > 0 {
				if !strings.Contains(findings[0].Description, tt.wantDescContains) {
					t.Errorf("expected description to contain %q, got %q", tt.wantDescContains, findings[0].Description)
				}
			}
		})
	}
}

func TestParseReviewFindings_IDAndStatus(t *testing.T) {
	t.Parallel()

	input := `ISSUE:
SEVERITY: critical
DESCRIPTION: SQL injection vulnerability in user input handler.

ISSUE:
DESCRIPTION: Missing error check on file open.`

	findings := ParseReviewFindings(input)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}

	// Both findings should have non-empty IDs.
	for i, f := range findings {
		if f.ID == "" {
			t.Errorf("finding %d: expected non-empty ID", i)
		}
		if f.Status != FindingStatusFound {
			t.Errorf("finding %d: expected status %q, got %q", i, FindingStatusFound, f.Status)
		}
	}

	// Different findings should have different IDs.
	if findings[0].ID == findings[1].ID {
		t.Errorf("expected different IDs for different findings, both got %q", findings[0].ID)
	}

	// ID should be deterministic: parse the same input again.
	findings2 := ParseReviewFindings(input)
	for i := range findings {
		if findings[i].ID != findings2[i].ID {
			t.Errorf("finding %d: ID not deterministic: %q vs %q", i, findings[i].ID, findings2[i].ID)
		}
	}

	// The finding with no severity should have defaulted to "major" before ID computation.
	expectedID := FindingID("major", findings[1].Description)
	if findings[1].ID != expectedID {
		t.Errorf("finding 1: expected ID %q (computed with default severity), got %q", expectedID, findings[1].ID)
	}
}

func TestParseVerifications(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantLen        int
		wantFindingIDs []string
		wantStatuses   []FindingStatus
		wantComments   []string
	}{
		{
			name: "SingleVerification",
			input: `VERIFICATION:
FINDING_ID: abc123
STATUS: fixed
COMMENT: The database connection is now properly closed in a defer.`,
			wantLen:        1,
			wantFindingIDs: []string{"abc123"},
			wantStatuses:   []FindingStatus{FindingStatusFixed},
			wantComments:   []string{"The database connection is now properly closed in a defer."},
		},
		{
			name: "MultipleVerifications",
			input: `VERIFICATION:
FINDING_ID: abc123
STATUS: fixed
COMMENT: Fixed by adding defer conn.Close().

VERIFICATION:
FINDING_ID: def456
STATUS: still_present
COMMENT: The input validation is still missing.

VERIFICATION:
FINDING_ID: ghi789
STATUS: regressed
COMMENT: The fix introduced a new nil pointer dereference.`,
			wantLen:        3,
			wantFindingIDs: []string{"abc123", "def456", "ghi789"},
			wantStatuses:   []FindingStatus{FindingStatusFixed, FindingStatusStillPresent, FindingStatusRegressed},
			wantComments: []string{
				"Fixed by adding defer conn.Close().",
				"The input validation is still missing.",
				"The fix introduced a new nil pointer dereference.",
			},
		},
		{
			name: "MixedWithIssueBlocks",
			input: `VERIFICATION:
FINDING_ID: abc123
STATUS: fixed
COMMENT: Resolved.

ISSUE:
SEVERITY: major
DESCRIPTION: New issue found in error handling.

VERIFICATION:
FINDING_ID: def456
STATUS: still_present
COMMENT: Still broken.`,
			wantLen:        2,
			wantFindingIDs: []string{"abc123", "def456"},
			wantStatuses:   []FindingStatus{FindingStatusFixed, FindingStatusStillPresent},
			wantComments:   []string{"Resolved.", "Still broken."},
		},
		{
			name: "MissingFindingID",
			input: `VERIFICATION:
STATUS: fixed
COMMENT: No ID provided so this should be skipped.`,
			wantLen: 0,
		},
		{
			name: "UnknownStatusDefaultsToStillPresent",
			input: `VERIFICATION:
FINDING_ID: abc123
STATUS: banana
COMMENT: Unknown status value.`,
			wantLen:        1,
			wantFindingIDs: []string{"abc123"},
			wantStatuses:   []FindingStatus{FindingStatusStillPresent},
			wantComments:   []string{"Unknown status value."},
		},
		{
			name:    "EmptyOutput",
			input:   "",
			wantLen: 0,
		},
		{
			name: "NoComment",
			input: `VERIFICATION:
FINDING_ID: abc123
STATUS: fixed`,
			wantLen:        1,
			wantFindingIDs: []string{"abc123"},
			wantStatuses:   []FindingStatus{FindingStatusFixed},
			wantComments:   []string{""},
		},
		{
			name: "VerificationStopsAtApproved",
			input: `VERIFICATION:
FINDING_ID: abc123
STATUS: fixed
COMMENT: All good.
APPROVED: Changes look great.`,
			wantLen:        1,
			wantFindingIDs: []string{"abc123"},
			wantStatuses:   []FindingStatus{FindingStatusFixed},
			wantComments:   []string{"All good."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			verifications := ParseVerifications(tt.input)
			if len(verifications) != tt.wantLen {
				t.Fatalf("expected %d verifications, got %d", tt.wantLen, len(verifications))
			}

			for i, wantID := range tt.wantFindingIDs {
				if verifications[i].FindingID != wantID {
					t.Errorf("verification %d: expected FindingID %q, got %q", i, wantID, verifications[i].FindingID)
				}
			}

			for i, wantStatus := range tt.wantStatuses {
				if verifications[i].Status != wantStatus {
					t.Errorf("verification %d: expected Status %q, got %q", i, wantStatus, verifications[i].Status)
				}
			}

			for i, wantComment := range tt.wantComments {
				if verifications[i].Comment != wantComment {
					t.Errorf("verification %d: expected Comment %q, got %q", i, wantComment, verifications[i].Comment)
				}
			}
		})
	}
}

func TestParseVerificationsCoexistWithFindings(t *testing.T) {
	t.Parallel()

	input := `VERIFICATION:
FINDING_ID: prior-1
STATUS: fixed
COMMENT: Fixed.

ISSUE:
SEVERITY: major
DESCRIPTION: New bug found in auth module.

VERIFICATION:
FINDING_ID: prior-2
STATUS: still_present
COMMENT: Still present.

ISSUE:
SEVERITY: minor
DESCRIPTION: Typo in variable name.`

	verifications := ParseVerifications(input)
	findings := ParseReviewFindings(input)

	if len(verifications) != 2 {
		t.Fatalf("expected 2 verifications, got %d", len(verifications))
	}
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}

	// Verify verifications are correct.
	if verifications[0].FindingID != "prior-1" || verifications[0].Status != FindingStatusFixed {
		t.Errorf("verification 0: unexpected values: %+v", verifications[0])
	}
	if verifications[1].FindingID != "prior-2" || verifications[1].Status != FindingStatusStillPresent {
		t.Errorf("verification 1: unexpected values: %+v", verifications[1])
	}

	// Verify findings are correct.
	if findings[0].Severity != "major" || !strings.Contains(findings[0].Description, "auth module") {
		t.Errorf("finding 0: unexpected values: %+v", findings[0])
	}
	if findings[1].Severity != "minor" || !strings.Contains(findings[1].Description, "Typo") {
		t.Errorf("finding 1: unexpected values: %+v", findings[1])
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
