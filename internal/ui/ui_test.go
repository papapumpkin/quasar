package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aaronsalm/quasar/internal/nebula"
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
		name   string
		substr string
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

func TestNebulaProgressBarLine_Basic(t *testing.T) {
	line := NebulaProgressBarLine(3, 7, 12, 8, 2.34)
	// Expected: [nebula] 3/7 phases complete | $2.34 spent

	checks := []struct {
		name   string
		substr string
	}{
		{"prefix", "[nebula]"},
		{"phase ratio", "3/7 phases complete"},
		{"cost", "$2.34 spent"},
	}

	for _, c := range checks {
		if !strings.Contains(line, c.substr) {
			t.Errorf("expected line to contain %s (%q), got: %s", c.name, c.substr, line)
		}
	}
}

func TestNebulaProgressBarLine_AllComplete(t *testing.T) {
	line := NebulaProgressBarLine(5, 5, 0, 10, 5.67)

	if !strings.Contains(line, "[nebula]") {
		t.Errorf("expected [nebula] prefix, got: %s", line)
	}
	if !strings.Contains(line, "5/5 phases complete") {
		t.Errorf("expected 5/5 phases complete, got: %s", line)
	}
	if !strings.Contains(line, "$5.67 spent") {
		t.Errorf("expected $5.67 spent, got: %s", line)
	}
}

func TestNebulaProgressBarLine_NoneComplete(t *testing.T) {
	line := NebulaProgressBarLine(0, 4, 4, 0, 0.0)

	if !strings.Contains(line, "[nebula]") {
		t.Errorf("expected [nebula] prefix, got: %s", line)
	}
	if !strings.Contains(line, "0/4 phases complete") {
		t.Errorf("expected 0/4 phases complete, got: %s", line)
	}
	if !strings.Contains(line, "$0.00 spent") {
		t.Errorf("expected $0.00 spent, got: %s", line)
	}
}

func TestNebulaProgressBarLine_ZeroTotal(t *testing.T) {
	// Edge case: no phases at all.
	line := NebulaProgressBarLine(0, 0, 0, 0, 0.0)

	if !strings.Contains(line, "[nebula]") {
		t.Errorf("expected [nebula] prefix, got: %s", line)
	}
	if !strings.Contains(line, "0/0 phases complete") {
		t.Errorf("expected 0/0 phases complete, got: %s", line)
	}
}

func TestNebulaProgressBar_WritesToStderr(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.NebulaProgressBar(2, 5, 3, 2, 1.50)
	})

	if len(output) == 0 {
		t.Error("expected NebulaProgressBar to write to stderr, got no output")
	}
	if !strings.Contains(output, "2/5 phases complete") {
		t.Errorf("expected output to contain phase ratio, got: %s", output)
	}
	if !strings.Contains(output, "$1.50 spent") {
		t.Errorf("expected output to contain cost, got: %s", output)
	}
	if !strings.Contains(output, "\r") {
		t.Errorf("expected output to contain carriage return, got: %q", output)
	}
}

func TestCycleSummary_OutputToStderr(t *testing.T) {
	// Verify that CycleSummary writes to stderr by capturing it.
	p := New()
	output := captureStderr(func() {
		p.CycleSummary(CycleSummaryData{
			Cycle:      1,
			MaxCycles:  1,
			Phase:      "code_complete",
			CostUSD:    0.01,
			DurationMs: 1000,
		})
	})

	if len(output) == 0 {
		t.Error("expected CycleSummary to write to stderr, got no output")
	}
}

func TestFormatDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero", 0, "0s"},
		{"sub-minute", 45 * time.Second, "45s"},
		{"one minute", time.Minute, "1m00s"},
		{"minutes and seconds", 4*time.Minute + 32*time.Second, "4m32s"},
		{"one hour", time.Hour, "1h00m00s"},
		{"hour and minutes", time.Hour + 30*time.Minute, "1h30m00s"},
		{"hour minutes seconds", time.Hour + 30*time.Minute + 5*time.Second, "1h30m05s"},
		{"multi-hour", 3*time.Hour + 15*time.Minute + 42*time.Second, "3h15m42s"},
		{"rounding", 59*time.Second + 500*time.Millisecond, "1m00s"},
		{"sub-second rounds down", 400 * time.Millisecond, "0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.d)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestPluralS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n    int
		want string
	}{
		{0, "s"},
		{1, ""},
		{2, "s"},
		{10, "s"},
	}

	for _, tt := range tests {
		got := pluralS(tt.n)
		if got != tt.want {
			t.Errorf("pluralS(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestNebulaAvgParallelism(t *testing.T) {
	t.Parallel()

	t.Run("empty waves", func(t *testing.T) {
		got := nebulaAvgParallelism(nil)
		if got != 0 {
			t.Errorf("nebulaAvgParallelism(nil) = %f, want 0", got)
		}
	})

	t.Run("single wave", func(t *testing.T) {
		waves := []nebula.WaveMetrics{
			{EffectiveParallelism: 3},
		}
		got := nebulaAvgParallelism(waves)
		if got != 3.0 {
			t.Errorf("nebulaAvgParallelism = %f, want 3.0", got)
		}
	})

	t.Run("multiple waves", func(t *testing.T) {
		waves := []nebula.WaveMetrics{
			{EffectiveParallelism: 3},
			{EffectiveParallelism: 2},
			{EffectiveParallelism: 2},
		}
		got := nebulaAvgParallelism(waves)
		// (3 + 2 + 2) / 3 = 2.333...
		want := 7.0 / 3.0
		if got != want {
			t.Errorf("nebulaAvgParallelism = %f, want %f", got, want)
		}
	})
}

func TestNebulaStatus_HistoryShowsMostRecent(t *testing.T) {
	p := New()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "history-test"},
		},
		Phases: []nebula.PhaseSpec{{ID: "p1"}, {ID: "p2"}},
	}
	state := &nebula.State{
		Phases: map[string]*nebula.PhaseState{
			"p1": {Status: nebula.PhaseStatusDone},
			"p2": {Status: nebula.PhaseStatusDone},
		},
	}

	base := time.Date(2026, 2, 10, 9, 0, 0, 0, time.UTC)

	// History is oldest-first (as stored by SaveMetrics).
	history := []nebula.HistorySummary{
		{StartedAt: base, TotalPhases: 4, TotalCostUSD: 5.00, Duration: 2 * time.Minute, TotalConflicts: 0},
		{StartedAt: base.Add(24 * time.Hour), TotalPhases: 5, TotalCostUSD: 7.00, Duration: 3 * time.Minute, TotalConflicts: 1},
		{StartedAt: base.Add(48 * time.Hour), TotalPhases: 6, TotalCostUSD: 9.00, Duration: 4 * time.Minute, TotalConflicts: 2},
		{StartedAt: base.Add(72 * time.Hour), TotalPhases: 7, TotalCostUSD: 11.00, Duration: 5 * time.Minute, TotalConflicts: 3},
		{StartedAt: base.Add(96 * time.Hour), TotalPhases: 8, TotalCostUSD: 13.00, Duration: 6 * time.Minute, TotalConflicts: 0},
	}

	output := captureStderr(func() {
		p.NebulaStatus(neb, state, nil, history)
	})

	// Should show last 3 runs, i.e. the 3rd, 4th, and 5th entries (most recent).
	if !strings.Contains(output, "last 3 runs") {
		t.Errorf("expected 'last 3 runs' in output, got:\n%s", output)
	}

	// Most recent three entries have 6, 7, and 8 phases.
	if !strings.Contains(output, "6 phases") {
		t.Errorf("expected '6 phases' (3rd newest) in output, got:\n%s", output)
	}
	if !strings.Contains(output, "7 phases") {
		t.Errorf("expected '7 phases' (2nd newest) in output, got:\n%s", output)
	}
	if !strings.Contains(output, "8 phases") {
		t.Errorf("expected '8 phases' (newest) in output, got:\n%s", output)
	}

	// Should NOT show the oldest entries (4 phases or 5 phases).
	if strings.Contains(output, "4 phases") {
		t.Errorf("should not show oldest entry (4 phases) in output, got:\n%s", output)
	}
	if strings.Contains(output, "5 phases") {
		t.Errorf("should not show 2nd oldest entry (5 phases) in output, got:\n%s", output)
	}
}

func TestNebulaStatus_NoMetrics(t *testing.T) {
	p := New()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "no-metrics"},
		},
		Phases: []nebula.PhaseSpec{{ID: "p1"}},
	}
	state := &nebula.State{
		TotalCostUSD: 1.23,
		Phases: map[string]*nebula.PhaseState{
			"p1": {Status: nebula.PhaseStatusDone},
		},
	}

	output := captureStderr(func() {
		p.NebulaStatus(neb, state, nil, nil)
	})

	// Graceful fallback: shows state-based info.
	if !strings.Contains(output, "no-metrics") {
		t.Errorf("expected nebula name in output, got:\n%s", output)
	}
	if !strings.Contains(output, "1 completed") {
		t.Errorf("expected '1 completed' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "$1.23") {
		t.Errorf("expected cost '$1.23' in output, got:\n%s", output)
	}
}

func TestNebulaStatus_HistoryFewerThan3(t *testing.T) {
	p := New()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "short-history"},
		},
	}
	state := &nebula.State{
		Phases: map[string]*nebula.PhaseState{},
	}

	base := time.Date(2026, 2, 14, 16, 0, 0, 0, time.UTC)
	history := []nebula.HistorySummary{
		{StartedAt: base, TotalPhases: 3, TotalCostUSD: 2.00, Duration: time.Minute, TotalConflicts: 1},
	}

	output := captureStderr(func() {
		p.NebulaStatus(neb, state, nil, history)
	})

	if !strings.Contains(output, "last 1 run)") {
		t.Errorf("expected 'last 1 run)' (singular) in output, got:\n%s", output)
	}
	if strings.Contains(output, "last 1 runs") {
		t.Errorf("expected singular 'run' not 'runs' for count 1, got:\n%s", output)
	}
	if !strings.Contains(output, "3 phases") {
		t.Errorf("expected '3 phases' in output, got:\n%s", output)
	}
}
