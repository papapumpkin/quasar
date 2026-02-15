package loop

import (
	"strings"
	"testing"

	"github.com/aaronsalm/quasar/internal/ui"
)

// noopUI satisfies ui.UI for tests without producing any output.
type noopUI struct{}

var _ ui.UI = (*noopUI)(nil)

func (n *noopUI) TaskStarted(string, string)          {}
func (n *noopUI) TaskComplete(string, float64)         {}
func (n *noopUI) CycleStart(int, int)                  {}
func (n *noopUI) AgentStart(string)                    {}
func (n *noopUI) AgentDone(string, float64, int64)     {}
func (n *noopUI) CycleSummary(ui.CycleSummaryData)     {}
func (n *noopUI) IssuesFound(int)                      {}
func (n *noopUI) Approved()                            {}
func (n *noopUI) MaxCyclesReached(int)                 {}
func (n *noopUI) BudgetExceeded(float64, float64)      {}
func (n *noopUI) Error(string)                         {}
func (n *noopUI) Info(string)                          {}

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

