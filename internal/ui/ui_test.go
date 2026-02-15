package ui

import (
	"os"
	"strings"
	"testing"
)

// captureStderr redirects os.Stderr to a pipe and returns the captured output.
func captureStderr(fn func()) string {
	r, w, _ := os.Pipe()
	orig := os.Stderr
	os.Stderr = w

	fn()

	w.Close()
	os.Stderr = orig

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	r.Close()
	return string(buf[:n])
}

func TestCycleSummary_CoderPhase(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.CycleSummary(CycleSummaryData{
			Cycle:        1,
			MaxCycles:    3,
			Phase:        "code_complete",
			CostUSD:      0.0523,
			TotalCostUSD: 0.0523,
			MaxBudgetUSD: 1.00,
			DurationMs:   12500,
		})
	})

	checks := []struct {
		name    string
		substr  string
	}{
		{"cycle number", "Cycle 1/3"},
		{"role", "coder"},
		{"phase cost", "$0.0523"},
		{"total cost", "$0.0523"},
		{"duration", "12.5s"},
		{"budget percent", "5%"},
	}

	for _, c := range checks {
		if !strings.Contains(output, c.substr) {
			t.Errorf("expected output to contain %s (%q), got:\n%s", c.name, c.substr, output)
		}
	}

	// Coder phase should NOT show outcome line.
	if strings.Contains(output, "outcome:") {
		t.Errorf("coder phase should not show outcome line, got:\n%s", output)
	}
}

func TestCycleSummary_ReviewerApproved(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.CycleSummary(CycleSummaryData{
			Cycle:        2,
			MaxCycles:    5,
			Phase:        "review_complete",
			CostUSD:      0.0312,
			TotalCostUSD: 0.1456,
			MaxBudgetUSD: 2.00,
			DurationMs:   8300,
			Approved:     true,
			IssueCount:   0,
		})
	})

	checks := []struct {
		name   string
		substr string
	}{
		{"cycle number", "Cycle 2/5"},
		{"role", "reviewer"},
		{"phase cost", "$0.0312"},
		{"total cost", "$0.1456"},
		{"duration", "8.3s"},
		{"approved", "approved"},
	}

	for _, c := range checks {
		if !strings.Contains(output, c.substr) {
			t.Errorf("expected output to contain %s (%q), got:\n%s", c.name, c.substr, output)
		}
	}
}

func TestCycleSummary_ReviewerIssues(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.CycleSummary(CycleSummaryData{
			Cycle:        1,
			MaxCycles:    3,
			Phase:        "review_complete",
			CostUSD:      0.0200,
			TotalCostUSD: 0.0700,
			MaxBudgetUSD: 1.50,
			DurationMs:   5000,
			Approved:     false,
			IssueCount:   3,
		})
	})

	checks := []struct {
		name   string
		substr string
	}{
		{"cycle number", "Cycle 1/3"},
		{"role", "reviewer"},
		{"issue count", "3 issue(s) found"},
		{"duration", "5.0s"},
	}

	for _, c := range checks {
		if !strings.Contains(output, c.substr) {
			t.Errorf("expected output to contain %s (%q), got:\n%s", c.name, c.substr, output)
		}
	}
}

func TestCycleSummary_NoBudget(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.CycleSummary(CycleSummaryData{
			Cycle:        1,
			MaxCycles:    1,
			Phase:        "code_complete",
			CostUSD:      0.0100,
			TotalCostUSD: 0.0100,
			MaxBudgetUSD: 0, // No budget limit.
			DurationMs:   2000,
		})
	})

	// Should not show budget percentage when no budget is set.
	if strings.Contains(output, "% of") {
		t.Errorf("expected no budget percentage when MaxBudgetUSD is 0, got:\n%s", output)
	}

	if !strings.Contains(output, "$0.0100") {
		t.Errorf("expected cost to appear, got:\n%s", output)
	}
}

func TestCycleSummary_OutputToStderr(t *testing.T) {
	// Verify that CycleSummary writes to stderr by capturing it.
	p := New()
	output := captureStderr(func() {
		p.CycleSummary(CycleSummaryData{
			Cycle:     1,
			MaxCycles: 1,
			Phase:     "code_complete",
			CostUSD:   0.01,
			DurationMs: 1000,
		})
	})

	if len(output) == 0 {
		t.Error("expected CycleSummary to write to stderr, got no output")
	}
}
