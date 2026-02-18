package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/nebula"
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

// --- Lifecycle method tests ---

func TestBanner(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.Banner()
	})

	checks := []struct {
		name   string
		substr string
	}{
		{"top border", "╔"},
		{"product name", "QUASAR"},
		{"subtitle", "dual-agent coordinator"},
		{"bottom border", "╚"},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(output, c.substr) {
				t.Errorf("expected output to contain %s (%q), got:\n%s", c.name, c.substr, output)
			}
		})
	}
}

func TestPrompt(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.Prompt()
	})

	if !strings.Contains(output, "quasar>") {
		t.Errorf("expected prompt to contain 'quasar>', got: %q", output)
	}
}

func TestCycleStart(t *testing.T) {
	p := New()

	tests := []struct {
		name      string
		cycle     int
		maxCycles int
		want      string
	}{
		{"first cycle", 1, 5, "cycle 1/5"},
		{"last cycle", 3, 3, "cycle 3/3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureStderr(func() {
				p.CycleStart(tt.cycle, tt.maxCycles)
			})
			if !strings.Contains(output, tt.want) {
				t.Errorf("expected output to contain %q, got: %q", tt.want, output)
			}
		})
	}
}

func TestAgentStart(t *testing.T) {
	p := New()

	tests := []struct {
		name string
		role string
		want string
	}{
		{"coder role", "coder", "coder"},
		{"reviewer role", "reviewer", "reviewer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureStderr(func() {
				p.AgentStart(tt.role)
			})
			if !strings.Contains(output, tt.want) {
				t.Errorf("expected output to contain %q, got: %q", tt.want, output)
			}
			if !strings.Contains(output, "working...") {
				t.Errorf("expected 'working...' in output, got: %q", output)
			}
		})
	}
}

func TestAgentDone(t *testing.T) {
	p := New()

	tests := []struct {
		name       string
		role       string
		costUSD    float64
		durationMs int64
		wantRole   string
		wantCost   string
		wantSecs   string
	}{
		{"coder done", "coder", 0.0523, 12500, "coder", "$0.0523", "12.5s"},
		{"reviewer done", "reviewer", 0.1000, 5000, "reviewer", "$0.1000", "5.0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureStderr(func() {
				p.AgentDone(tt.role, tt.costUSD, tt.durationMs)
			})
			checks := []string{tt.wantRole, tt.wantCost, tt.wantSecs, "done"}
			for _, want := range checks {
				if !strings.Contains(output, want) {
					t.Errorf("expected output to contain %q, got: %q", want, output)
				}
			}
		})
	}
}

func TestIssuesFound(t *testing.T) {
	p := New()

	tests := []struct {
		name  string
		count int
		want  string
	}{
		{"single issue", 1, "1 issue(s) found"},
		{"multiple issues", 5, "5 issue(s) found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureStderr(func() {
				p.IssuesFound(tt.count)
			})
			if !strings.Contains(output, tt.want) {
				t.Errorf("expected output to contain %q, got: %q", tt.want, output)
			}
			if !strings.Contains(output, "sending back to coder") {
				t.Errorf("expected 'sending back to coder' in output, got: %q", output)
			}
		})
	}
}

func TestApproved(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.Approved()
	})

	if !strings.Contains(output, "APPROVED") {
		t.Errorf("expected 'APPROVED' in output, got: %q", output)
	}
	if !strings.Contains(output, "reviewer is satisfied") {
		t.Errorf("expected 'reviewer is satisfied' in output, got: %q", output)
	}
}

func TestMaxCyclesReached(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.MaxCyclesReached(5)
	})

	if !strings.Contains(output, "max cycles reached (5)") {
		t.Errorf("expected 'max cycles reached (5)' in output, got: %q", output)
	}
	if !strings.Contains(output, "stopping") {
		t.Errorf("expected 'stopping' in output, got: %q", output)
	}
}

func TestBudgetExceeded(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.BudgetExceeded(1.50, 1.00)
	})

	if !strings.Contains(output, "budget exceeded") {
		t.Errorf("expected 'budget exceeded' in output, got: %q", output)
	}
	if !strings.Contains(output, "$1.50") {
		t.Errorf("expected '$1.50' (spent) in output, got: %q", output)
	}
	if !strings.Contains(output, "$1.00") {
		t.Errorf("expected '$1.00' (limit) in output, got: %q", output)
	}
}

func TestError(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.Error("something went wrong")
	})

	if !strings.Contains(output, "error:") {
		t.Errorf("expected 'error:' prefix in output, got: %q", output)
	}
	if !strings.Contains(output, "something went wrong") {
		t.Errorf("expected error message in output, got: %q", output)
	}
}

func TestInfo(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.Info("loading configuration")
	})

	if !strings.Contains(output, "loading configuration") {
		t.Errorf("expected info message in output, got: %q", output)
	}
}

// --- No-op method tests ---

func TestAgentOutput_NoOp(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.AgentOutput("coder", 1, "some output text")
	})

	if len(output) != 0 {
		t.Errorf("expected AgentOutput to be a no-op (no output), got: %q", output)
	}
}

func TestBeadUpdate_NoOp(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.BeadUpdate("bead-123", "test task", "open", []BeadChild{
			{ID: "child-1", Title: "subtask", Status: "open"},
		})
	})

	if len(output) != 0 {
		t.Errorf("expected BeadUpdate to be a no-op (no output), got: %q", output)
	}
}

func TestRefactorApplied_NoOp(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.RefactorApplied("phase-1")
	})

	if len(output) != 0 {
		t.Errorf("expected RefactorApplied to be a no-op (no output), got: %q", output)
	}
}

// --- Task lifecycle tests ---

func TestTaskStarted(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.TaskStarted("bead-abc", "implement feature X")
	})

	if !strings.Contains(output, "task") {
		t.Errorf("expected 'task' in output, got: %q", output)
	}
	if !strings.Contains(output, "bead-abc") {
		t.Errorf("expected bead ID in output, got: %q", output)
	}
	if !strings.Contains(output, "implement feature X") {
		t.Errorf("expected title in output, got: %q", output)
	}
}

func TestTaskComplete(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.TaskComplete("bead-abc", 1.2345)
	})

	if !strings.Contains(output, "task complete") {
		t.Errorf("expected 'task complete' in output, got: %q", output)
	}
	if !strings.Contains(output, "bead-abc") {
		t.Errorf("expected bead ID in output, got: %q", output)
	}
	if !strings.Contains(output, "$1.2345") {
		t.Errorf("expected total cost in output, got: %q", output)
	}
}

// --- Help and status tests ---

func TestShowHelp(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.ShowHelp()
	})

	checks := []struct {
		name   string
		substr string
	}{
		{"commands header", "Commands:"},
		{"help command", "help"},
		{"status command", "status"},
		{"quit command", "quit"},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(output, c.substr) {
				t.Errorf("expected output to contain %q, got:\n%s", c.substr, output)
			}
		})
	}
}

func TestShowStatus(t *testing.T) {
	p := New()

	t.Run("with model", func(t *testing.T) {
		output := captureStderr(func() {
			p.ShowStatus(5, 2.50, "claude-sonnet")
		})

		checks := []string{"config:", "max cycles:  5", "max budget:  $2.50", "model:       claude-sonnet"}
		for _, want := range checks {
			if !strings.Contains(output, want) {
				t.Errorf("expected output to contain %q, got:\n%s", want, output)
			}
		}
	})

	t.Run("empty model shows default", func(t *testing.T) {
		output := captureStderr(func() {
			p.ShowStatus(3, 1.00, "")
		})

		if !strings.Contains(output, "(default)") {
			t.Errorf("expected '(default)' for empty model, got:\n%s", output)
		}
		if !strings.Contains(output, "max cycles:  3") {
			t.Errorf("expected 'max cycles:  3', got:\n%s", output)
		}
	})
}

// --- ANSICursorUp test ---

func TestANSICursorUp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n    int
		want string
	}{
		{1, "\033[1A"},
		{5, "\033[5A"},
		{0, "\033[0A"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			got := ANSICursorUp(tt.n)
			if got != tt.want {
				t.Errorf("ANSICursorUp(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

// --- NebulaProgressBarDone test ---

func TestNebulaProgressBarDone(t *testing.T) {
	p := New()
	output := captureStderr(func() {
		p.NebulaProgressBarDone()
	})

	// Should write a newline.
	if !strings.Contains(output, "\n") {
		t.Errorf("expected NebulaProgressBarDone to write a newline, got: %q", output)
	}
}

// --- Nebula output tests ---

func TestNebulaValidateResult(t *testing.T) {
	p := New()

	t.Run("no errors", func(t *testing.T) {
		output := captureStderr(func() {
			p.NebulaValidateResult("my-nebula", 3, nil)
		})

		if !strings.Contains(output, "my-nebula") {
			t.Errorf("expected nebula name in output, got: %q", output)
		}
		if !strings.Contains(output, "3 phase(s)") {
			t.Errorf("expected phase count in output, got: %q", output)
		}
		if !strings.Contains(output, "no errors") {
			t.Errorf("expected 'no errors' in output, got: %q", output)
		}
	})

	t.Run("with errors", func(t *testing.T) {
		errs := []nebula.ValidationError{
			{PhaseID: "p1", SourceFile: "phases/p1.yaml", Err: fmt.Errorf("missing title")},
			{SourceFile: "nebula.yaml", Err: fmt.Errorf("invalid name")},
		}
		output := captureStderr(func() {
			p.NebulaValidateResult("bad-nebula", 0, errs)
		})

		if !strings.Contains(output, "bad-nebula") {
			t.Errorf("expected nebula name in output, got: %q", output)
		}
		if !strings.Contains(output, "2 error(s)") {
			t.Errorf("expected '2 error(s)' in output, got: %q", output)
		}
		if !strings.Contains(output, "missing title") {
			t.Errorf("expected first error detail in output, got: %q", output)
		}
		if !strings.Contains(output, "invalid name") {
			t.Errorf("expected second error detail in output, got: %q", output)
		}
	})
}

func TestNebulaPlan(t *testing.T) {
	p := New()

	t.Run("with actions", func(t *testing.T) {
		plan := &nebula.Plan{
			NebulaName: "test-nebula",
			Actions: []nebula.Action{
				{PhaseID: "phase-a", Type: nebula.ActionCreate, Reason: "new phase"},
				{PhaseID: "phase-b", Type: nebula.ActionUpdate, Reason: "changed spec"},
				{PhaseID: "phase-c", Type: nebula.ActionSkip, Reason: "already done"},
				{PhaseID: "phase-d", Type: nebula.ActionClose, Reason: "removed from spec"},
				{PhaseID: "phase-e", Type: nebula.ActionRetry, Reason: "previously failed"},
			},
		}

		output := captureStderr(func() {
			p.NebulaPlan(plan)
		})

		checks := []string{
			"test-nebula",
			"phase-a", "new phase",
			"phase-b", "changed spec",
			"phase-c", "already done",
			"phase-d", "removed from spec",
			"phase-e", "previously failed",
		}
		for _, want := range checks {
			if !strings.Contains(output, want) {
				t.Errorf("expected output to contain %q, got:\n%s", want, output)
			}
		}
	})

	t.Run("no actions", func(t *testing.T) {
		plan := &nebula.Plan{
			NebulaName: "empty-nebula",
			Actions:    nil,
		}

		output := captureStderr(func() {
			p.NebulaPlan(plan)
		})

		if !strings.Contains(output, "empty-nebula") {
			t.Errorf("expected nebula name in output, got: %q", output)
		}
		if !strings.Contains(output, "(no actions)") {
			t.Errorf("expected '(no actions)' in output, got: %q", output)
		}
	})
}

func TestNebulaApplyDone(t *testing.T) {
	p := New()

	plan := &nebula.Plan{
		NebulaName: "apply-test",
		Actions: []nebula.Action{
			{PhaseID: "p1", Type: nebula.ActionCreate},
			{PhaseID: "p2", Type: nebula.ActionCreate},
			{PhaseID: "p3", Type: nebula.ActionUpdate},
			{PhaseID: "p4", Type: nebula.ActionSkip},
			{PhaseID: "p5", Type: nebula.ActionClose},
			{PhaseID: "p6", Type: nebula.ActionRetry},
		},
	}

	output := captureStderr(func() {
		p.NebulaApplyDone(plan)
	})

	checks := []string{
		"apply complete",
		"created: 2",
		"updated: 1",
		"retried: 1",
		"closed: 1",
		"skipped: 1",
	}
	for _, want := range checks {
		if !strings.Contains(output, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, output)
		}
	}
}

func TestNebulaWorkerResults(t *testing.T) {
	p := New()

	t.Run("mixed success and failure", func(t *testing.T) {
		results := []nebula.WorkerResult{
			{PhaseID: "p1", BeadID: "bead-001", Err: nil},
			{PhaseID: "p2", Err: fmt.Errorf("build failed")},
			{PhaseID: "p3", BeadID: "bead-003", Err: nil, Report: &agent.ReviewReport{
				Satisfaction:     "high",
				Risk:             "low",
				NeedsHumanReview: false,
				Summary:          "looks good",
			}},
		}

		output := captureStderr(func() {
			p.NebulaWorkerResults(results)
		})

		checks := []string{
			"worker results:",
			"p1", "bead-001",
			"p2", "build failed",
			"p3", "bead-003",
			"satisfaction:", "high",
			"risk:", "low",
			"summary:", "looks good",
		}
		for _, want := range checks {
			if !strings.Contains(output, want) {
				t.Errorf("expected output to contain %q, got:\n%s", want, output)
			}
		}
	})
}

func TestReviewReport(t *testing.T) {
	p := New()

	t.Run("without human review", func(t *testing.T) {
		report := &agent.ReviewReport{
			Satisfaction:     "medium",
			Risk:             "high",
			NeedsHumanReview: false,
			Summary:          "needs some work",
		}

		output := captureStderr(func() {
			p.ReviewReport("phase-x", report)
		})

		checks := []string{
			"report for phase-x",
			"satisfaction:  medium",
			"risk:          high",
			"human review:  no",
			"summary:       needs some work",
		}
		for _, want := range checks {
			if !strings.Contains(output, want) {
				t.Errorf("expected output to contain %q, got:\n%s", want, output)
			}
		}
	})

	t.Run("with human review", func(t *testing.T) {
		report := &agent.ReviewReport{
			Satisfaction:     "low",
			Risk:             "critical",
			NeedsHumanReview: true,
			Summary:          "security concern",
		}

		output := captureStderr(func() {
			p.ReviewReport("phase-y", report)
		})

		if !strings.Contains(output, "human review:  ") {
			t.Errorf("expected human review line in output, got:\n%s", output)
		}
		// When NeedsHumanReview is true, the output should contain "yes".
		if !strings.Contains(output, "yes") {
			t.Errorf("expected 'yes' for human review, got:\n%s", output)
		}
	})
}

func TestNebulaShow(t *testing.T) {
	p := New()

	t.Run("basic nebula with phases", func(t *testing.T) {
		neb := &nebula.Nebula{
			Manifest: nebula.Manifest{
				Nebula: nebula.Info{
					Name:        "show-test",
					Description: "A test nebula for display",
				},
			},
			Phases: []nebula.PhaseSpec{
				{ID: "p1", Title: "First phase"},
				{ID: "p2", Title: "Second phase", DependsOn: []string{"p1"}},
			},
		}
		state := &nebula.State{
			Phases: map[string]*nebula.PhaseState{
				"p1": {Status: nebula.PhaseStatusDone, BeadID: "bead-100"},
				"p2": {Status: nebula.PhaseStatusInProgress},
			},
		}

		output := captureStderr(func() {
			p.NebulaShow(neb, state)
		})

		checks := []string{
			"show-test",
			"A test nebula for display",
			"phases: 2",
			"p1", "First phase", "done", "bead:bead-100",
			"p2", "Second phase", "in_progress", "depends:[p1]",
		}
		for _, want := range checks {
			if !strings.Contains(output, want) {
				t.Errorf("expected output to contain %q, got:\n%s", want, output)
			}
		}
	})

	t.Run("with execution config", func(t *testing.T) {
		neb := &nebula.Nebula{
			Manifest: nebula.Manifest{
				Nebula: nebula.Info{Name: "exec-test"},
				Execution: nebula.Execution{
					MaxWorkers:      4,
					MaxReviewCycles: 3,
					MaxBudgetUSD:    10.00,
					Model:           "claude-opus",
				},
			},
			Phases: []nebula.PhaseSpec{{ID: "p1", Title: "Only phase"}},
		}
		state := &nebula.State{
			Phases: map[string]*nebula.PhaseState{},
		}

		output := captureStderr(func() {
			p.NebulaShow(neb, state)
		})

		checks := []string{
			"execution:",
			"max workers:       4",
			"max review cycles: 3",
			"max budget:        $10.00",
			"model:             claude-opus",
		}
		for _, want := range checks {
			if !strings.Contains(output, want) {
				t.Errorf("expected output to contain %q, got:\n%s", want, output)
			}
		}
	})

	t.Run("with context", func(t *testing.T) {
		neb := &nebula.Nebula{
			Manifest: nebula.Manifest{
				Nebula: nebula.Info{Name: "ctx-test"},
				Context: nebula.Context{
					Repo:        "github.com/example/repo",
					WorkingDir:  "src/",
					Goals:       []string{"improve coverage", "fix bugs"},
					Constraints: []string{"no external deps"},
				},
			},
			Phases: []nebula.PhaseSpec{{ID: "p1", Title: "Only phase"}},
		}
		state := &nebula.State{
			Phases: map[string]*nebula.PhaseState{},
		}

		output := captureStderr(func() {
			p.NebulaShow(neb, state)
		})

		checks := []string{
			"context:",
			"repo: github.com/example/repo",
			"working dir: src/",
			"goals:",
			"- improve coverage",
			"- fix bugs",
			"constraints:",
			"- no external deps",
		}
		for _, want := range checks {
			if !strings.Contains(output, want) {
				t.Errorf("expected output to contain %q, got:\n%s", want, output)
			}
		}
	})

	t.Run("with dependencies", func(t *testing.T) {
		neb := &nebula.Nebula{
			Manifest: nebula.Manifest{
				Nebula: nebula.Info{Name: "deps-test"},
				Dependencies: nebula.Dependencies{
					RequiresBeads:   []string{"bead-aaa", "bead-bbb"},
					RequiresNebulae: []string{"other-nebula"},
				},
			},
			Phases: []nebula.PhaseSpec{{ID: "p1", Title: "Only phase"}},
		}
		state := &nebula.State{
			Phases: map[string]*nebula.PhaseState{},
		}

		output := captureStderr(func() {
			p.NebulaShow(neb, state)
		})

		checks := []string{
			"dependencies:",
			"requires beads:   bead-aaa, bead-bbb",
			"requires nebulae: other-nebula",
		}
		for _, want := range checks {
			if !strings.Contains(output, want) {
				t.Errorf("expected output to contain %q, got:\n%s", want, output)
			}
		}
	})

	t.Run("phase with review report", func(t *testing.T) {
		neb := &nebula.Nebula{
			Manifest: nebula.Manifest{
				Nebula: nebula.Info{Name: "report-test"},
			},
			Phases: []nebula.PhaseSpec{{ID: "p1", Title: "Reviewed phase"}},
		}
		state := &nebula.State{
			Phases: map[string]*nebula.PhaseState{
				"p1": {
					Status: nebula.PhaseStatusDone,
					BeadID: "bead-r1",
					Report: &agent.ReviewReport{
						Satisfaction:     "high",
						Risk:             "low",
						NeedsHumanReview: false,
					},
				},
			},
		}

		output := captureStderr(func() {
			p.NebulaShow(neb, state)
		})

		checks := []string{
			"satisfaction:high",
			"risk:low",
			"human-review:false",
		}
		for _, want := range checks {
			if !strings.Contains(output, want) {
				t.Errorf("expected output to contain %q, got:\n%s", want, output)
			}
		}
	})

	t.Run("pending phase no state", func(t *testing.T) {
		neb := &nebula.Nebula{
			Manifest: nebula.Manifest{
				Nebula: nebula.Info{Name: "pending-test"},
			},
			Phases: []nebula.PhaseSpec{{ID: "p1", Title: "Pending phase"}},
		}
		state := &nebula.State{
			Phases: map[string]*nebula.PhaseState{},
		}

		output := captureStderr(func() {
			p.NebulaShow(neb, state)
		})

		if !strings.Contains(output, "pending") {
			t.Errorf("expected 'pending' status for unstarted phase, got:\n%s", output)
		}
	})
}

// --- NebulaStatus with metrics tests ---

func TestNebulaStatus_WithMetrics(t *testing.T) {
	p := New()

	started := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)
	completed := started.Add(5*time.Minute + 30*time.Second)

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "metrics-test"},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "p1"}, {ID: "p2"}, {ID: "p3"},
		},
	}
	state := &nebula.State{
		Phases: map[string]*nebula.PhaseState{
			"p1": {Status: nebula.PhaseStatusDone},
			"p2": {Status: nebula.PhaseStatusDone},
			"p3": {Status: nebula.PhaseStatusFailed},
		},
	}
	metrics := &nebula.Metrics{
		StartedAt:      started,
		CompletedAt:    completed,
		TotalCostUSD:   4.56,
		TotalConflicts: 2,
		TotalRestarts:  1,
		Waves: []nebula.WaveMetrics{
			{WaveNumber: 1, PhaseCount: 2, EffectiveParallelism: 2, TotalDuration: 3 * time.Minute},
			{WaveNumber: 2, PhaseCount: 1, EffectiveParallelism: 1, TotalDuration: 2 * time.Minute},
		},
		Phases: []nebula.PhaseMetrics{
			{PhaseID: "p1", Duration: 3 * time.Minute, CostUSD: 2.00, CyclesUsed: 2, Satisfaction: "high"},
			{PhaseID: "p2", Duration: 2 * time.Minute, CostUSD: 1.50, CyclesUsed: 1, Satisfaction: "medium"},
			{PhaseID: "p3", Duration: 30 * time.Second, CostUSD: 1.06, CyclesUsed: 1},
		},
	}

	output := captureStderr(func() {
		p.NebulaStatus(neb, state, metrics, nil)
	})

	checks := []string{
		"metrics-test",
		"last run",
		"2 completed",
		"1 failed",
		"1 restarts",
		"2 (avg effective parallelism:",
		"$4.56",
		"5m30s",
		"Conflicts: 2",
		"Wave breakdown:",
		"Wave 1: 2 phases",
		"Wave 2: 1 phases",
		"Slowest phases:",
		"p1",
		"p2",
	}
	for _, want := range checks {
		if !strings.Contains(output, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, output)
		}
	}
}

func TestNebulaStatus_InProgress(t *testing.T) {
	p := New()

	started := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "in-progress-test"},
		},
		Phases: []nebula.PhaseSpec{{ID: "p1"}},
	}
	state := &nebula.State{
		Phases: map[string]*nebula.PhaseState{},
	}
	metrics := &nebula.Metrics{
		StartedAt: started,
		// CompletedAt is zero — still in progress.
	}

	output := captureStderr(func() {
		p.NebulaStatus(neb, state, metrics, nil)
	})

	if !strings.Contains(output, "in progress") {
		t.Errorf("expected 'in progress' in output, got:\n%s", output)
	}
}

func TestNebulaStatus_WaveScopeSerialization(t *testing.T) {
	p := New()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "scope-test"},
		},
		Phases: []nebula.PhaseSpec{{ID: "p1"}, {ID: "p2"}},
	}
	state := &nebula.State{
		Phases: map[string]*nebula.PhaseState{},
	}

	started := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)
	metrics := &nebula.Metrics{
		StartedAt:   started,
		CompletedAt: started.Add(time.Minute),
		Waves: []nebula.WaveMetrics{
			{WaveNumber: 1, PhaseCount: 3, EffectiveParallelism: 2, TotalDuration: time.Minute},
		},
	}

	output := captureStderr(func() {
		p.NebulaStatus(neb, state, metrics, nil)
	})

	// EffectiveParallelism (2) < PhaseCount (3), so "(scope serialization)" should appear.
	if !strings.Contains(output, "(scope serialization)") {
		t.Errorf("expected '(scope serialization)' note in output, got:\n%s", output)
	}
}
