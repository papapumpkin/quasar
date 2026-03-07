package tui

import (
	"strings"
	"testing"
	"time"
)

func TestExtractAgentSummary_StructuredSection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantSub string
	}{
		{
			name:    "## Summary heading",
			input:   "lots of code\n## Summary\nFixed the login bug\nUpdated tests\n## Files\nfoo.go",
			wantSub: "Fixed the login bug",
		},
		{
			name:    "## What I changed heading",
			input:   "preamble\n## What I changed\nRefactored auth module\nAdded error handling\n",
			wantSub: "Refactored auth module",
		},
		{
			name:    "## Changes heading",
			input:   "preamble\n## Changes\nLine 1\nLine 2\nLine 3\nLine 4",
			wantSub: "Line 1",
		},
		{
			name:    "case insensitive",
			input:   "preamble\n## SUMMARY\nDid the thing\n",
			wantSub: "Did the thing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ExtractAgentSummary(tt.input)
			if !strings.Contains(result, tt.wantSub) {
				t.Errorf("ExtractAgentSummary() = %q, want substring %q", result, tt.wantSub)
			}
		})
	}
}

func TestExtractAgentSummary_Fallback(t *testing.T) {
	t.Parallel()
	input := "line1\nline2\nline3\nline4\nline5"
	result := ExtractAgentSummary(input)
	if !strings.Contains(result, "line5") {
		t.Error("fallback should include last line")
	}
	if !strings.Contains(result, "line3") {
		t.Error("fallback should include third-to-last line")
	}
	// Should not contain earlier lines (max 3 lines).
	lines := strings.Split(result, "\n")
	if len(lines) > maxSummaryLines {
		t.Errorf("summary has %d lines, want at most %d", len(lines), maxSummaryLines)
	}
}

func TestExtractAgentSummary_Empty(t *testing.T) {
	t.Parallel()
	if result := ExtractAgentSummary(""); result != "" {
		t.Errorf("expected empty result for empty input, got %q", result)
	}
}

func TestExtractAgentSummary_MaxLines(t *testing.T) {
	t.Parallel()
	input := "## Summary\nLine 1\nLine 2\nLine 3\nLine 4\nLine 5"
	result := ExtractAgentSummary(input)
	lines := strings.Split(result, "\n")
	if len(lines) > maxSummaryLines {
		t.Errorf("summary has %d lines, want at most %d", len(lines), maxSummaryLines)
	}
}

func TestExtractAgentSummary_StopsAtBlankLine(t *testing.T) {
	t.Parallel()
	input := "## Summary\nLine 1\nLine 2\n\nLine after blank"
	result := ExtractAgentSummary(input)
	if strings.Contains(result, "Line after blank") {
		t.Error("should stop at blank line after collecting content")
	}
	if !strings.Contains(result, "Line 1") {
		t.Error("should include Line 1")
	}
}

func TestExtractAgentSummary_StopsAtNextHeading(t *testing.T) {
	t.Parallel()
	input := "## Summary\nDid the thing\n## Files\nfoo.go"
	result := ExtractAgentSummary(input)
	if strings.Contains(result, "foo.go") {
		t.Error("should stop at next heading")
	}
	if !strings.Contains(result, "Did the thing") {
		t.Error("should include content before heading")
	}
}

func TestSatisfactionFromReview(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		approved bool
		issues   int
		want     Satisfaction
	}{
		{"approved", true, 0, SatisfactionGreen},
		{"approved with issues", true, 3, SatisfactionGreen},
		{"few issues", false, 1, SatisfactionYellow},
		{"two issues", false, 2, SatisfactionYellow},
		{"many issues", false, 5, SatisfactionRed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SatisfactionFromReview(tt.approved, tt.issues)
			if got != tt.want {
				t.Errorf("SatisfactionFromReview(%v, %d) = %d, want %d", tt.approved, tt.issues, got, tt.want)
			}
		})
	}
}

func TestSatisfactionBadge(t *testing.T) {
	t.Parallel()
	tests := []struct {
		sat     Satisfaction
		wantSub string
	}{
		{SatisfactionGreen, "approved"},
		{SatisfactionYellow, "issues"},
		{SatisfactionRed, "critical"},
		{SatisfactionNone, ""},
	}
	for _, tt := range tests {
		t.Run(tt.wantSub, func(t *testing.T) {
			t.Parallel()
			result := SatisfactionBadge(tt.sat)
			if tt.wantSub == "" {
				if result != "" {
					t.Errorf("SatisfactionBadge(%d) = %q, want empty", tt.sat, result)
				}
				return
			}
			if !strings.Contains(result, tt.wantSub) {
				t.Errorf("SatisfactionBadge(%d) = %q, want substring %q", tt.sat, result, tt.wantSub)
			}
		})
	}
}

func TestFormatFileSummary(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		if result := FormatFileSummary(nil); result != "" {
			t.Errorf("expected empty for nil files, got %q", result)
		}
	})

	t.Run("few files", func(t *testing.T) {
		t.Parallel()
		files := []FileStatEntry{
			{Path: "foo.go", Additions: 10, Deletions: 3},
			{Path: "bar.go", Additions: 5, Deletions: 0},
		}
		result := FormatFileSummary(files)
		if !strings.Contains(result, "foo.go") {
			t.Error("should contain foo.go")
		}
		if !strings.Contains(result, "bar.go") {
			t.Error("should contain bar.go")
		}
	})

	t.Run("more than 5 files truncates", func(t *testing.T) {
		t.Parallel()
		files := make([]FileStatEntry, 8)
		for i := range files {
			files[i] = FileStatEntry{Path: "file" + strings.Repeat("x", i) + ".go", Additions: i, Deletions: i}
		}
		result := FormatFileSummary(files)
		if !strings.Contains(result, "more files") {
			t.Error("should show truncation indicator for >5 files")
		}
	})
}

func TestFormatPhaseSummary(t *testing.T) {
	t.Parallel()
	p := PhaseEntry{
		ID:          "setup",
		Title:       "Setup models",
		Status:      PhaseDone,
		CostUSD:     1.23,
		Cycles:      3,
		StartedAt:   time.Now().Add(-5 * time.Minute),
		CompletedAt: time.Now(),
	}
	lv := NewLoopView()
	lv.Approved = true
	lv.StartCycle(1)
	lv.StartAgent("coder")
	lv.FinishAgent("coder", 0.5, 5000)
	lv.SetAgentOutput("coder", 1, "## Summary\nFixed the login bug")
	lv.StartAgent("reviewer")
	lv.FinishAgent("reviewer", 0.3, 3000)
	lv.SetAgentOutput("reviewer", 1, "## Summary\nLooks good, approved")

	result := FormatPhaseSummary(p, &lv)
	if !strings.Contains(result, "3") {
		t.Error("should contain cycle count")
	}
	if !strings.Contains(result, "$1.23") {
		t.Error("should contain cost")
	}
	if !strings.Contains(result, "approved") {
		t.Error("should contain assessment")
	}
	if !strings.Contains(result, "Looks good") {
		t.Error("should contain reviewer summary")
	}
}

func TestFormatPhaseSummary_NilLoopView(t *testing.T) {
	t.Parallel()
	p := PhaseEntry{
		ID:      "setup",
		Status:  PhaseDone,
		CostUSD: 0.5,
		Cycles:  1,
	}
	result := FormatPhaseSummary(p, nil)
	if !strings.Contains(result, "1") {
		t.Error("should contain cycle count")
	}
	if !strings.Contains(result, "$0.50") {
		t.Error("should contain cost")
	}
}

func TestSetAgentOutput_ExtractsSummary(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	lv.StartCycle(1)
	lv.StartAgent("coder")

	lv.SetAgentOutput("coder", 1, "## Summary\nFixed the login bug\nAdded tests")

	agent := lv.Cycles[0].Agents[0]
	if agent.Summary == "" {
		t.Error("expected summary to be extracted")
	}
	if !strings.Contains(agent.Summary, "Fixed the login bug") {
		t.Errorf("summary %q should contain 'Fixed the login bug'", agent.Summary)
	}
}

func TestSetSatisfaction(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	lv.StartCycle(1)
	lv.StartAgent("coder")
	lv.FinishAgent("coder", 0.5, 5000)
	lv.StartAgent("reviewer")
	lv.FinishAgent("reviewer", 0.3, 3000)

	lv.SetSatisfaction(SatisfactionGreen)

	// Find the reviewer agent.
	found := false
	for _, c := range lv.Cycles {
		for _, a := range c.Agents {
			if a.Role == "reviewer" {
				if a.Satisfaction != SatisfactionGreen {
					t.Errorf("reviewer satisfaction = %d, want %d", a.Satisfaction, SatisfactionGreen)
				}
				found = true
			}
		}
	}
	if !found {
		t.Error("reviewer agent not found")
	}
}

func TestSetSatisfaction_EmptyCycles(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	// Should not panic.
	lv.SetSatisfaction(SatisfactionGreen)
}

func TestLoopViewView_ShowsSummary(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	lv.Width = 80
	lv.Cycles = []CycleEntry{
		{
			Number: 1,
			Agents: []AgentEntry{
				{
					Role:       "coder",
					Done:       true,
					CostUSD:    0.45,
					DurationMs: 12300,
					Summary:    "Fixed the login bug",
				},
			},
		},
	}

	view := lv.View()
	if !strings.Contains(view, "Fixed the login bug") {
		t.Error("expected summary text in cycle view")
	}
}

func TestLoopViewView_ShowsSatisfactionBadge(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	lv.Width = 80
	lv.Cycles = []CycleEntry{
		{
			Number: 1,
			Agents: []AgentEntry{
				{
					Role:         "reviewer",
					Done:         true,
					CostUSD:      0.32,
					DurationMs:   8100,
					IssueCount:   0,
					Satisfaction: SatisfactionGreen,
				},
			},
		},
	}

	view := lv.View()
	if !strings.Contains(view, "approved") {
		t.Error("expected satisfaction badge in reviewer row")
	}
}

func TestSetPhaseCompletionNote(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.InitPhases([]PhaseInfo{
		{ID: "setup", Title: "Setup"},
	})

	nv.SetPhaseCompletionNote("setup", "all tests pass")
	if nv.Phases[0].CompletionNote != "all tests pass" {
		t.Errorf("CompletionNote = %q, want %q", nv.Phases[0].CompletionNote, "all tests pass")
	}
}

func TestSetPhaseCompletionNote_NonExistent(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	// Should not panic for unknown phase.
	nv.SetPhaseCompletionNote("nonexistent", "note")
}

func TestExtractFromSection_SkipsBlankLinesBeforeContent(t *testing.T) {
	t.Parallel()
	input := "## Summary\n\n\nActual content here"
	result := ExtractAgentSummary(input)
	if !strings.Contains(result, "Actual content here") {
		t.Errorf("should skip blank lines before content, got %q", result)
	}
}

func TestAggregateFiles(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	lv.StartCycle(1)
	lv.StartAgent("coder")
	lv.FinishAgent("coder", 0.5, 5000)
	lv.SetAgentDiffFiles("coder", 1, []FileStatEntry{
		{Path: "foo.go", Additions: 10, Deletions: 3},
	}, "abc", "def", "/tmp")
	lv.StartCycle(2)
	lv.StartAgent("coder")
	lv.FinishAgent("coder", 0.5, 5000)
	lv.SetAgentDiffFiles("coder", 2, []FileStatEntry{
		{Path: "foo.go", Additions: 12, Deletions: 5},
		{Path: "bar.go", Additions: 20, Deletions: 0},
	}, "def", "ghi", "/tmp")

	files := aggregateFiles(&lv)
	if len(files) != 2 {
		t.Errorf("aggregateFiles returned %d files, want 2", len(files))
	}
	// foo.go should have latest stats.
	if files[0].Path != "foo.go" || files[0].Additions != 12 {
		t.Errorf("foo.go stats = +%d, want +12", files[0].Additions)
	}
	if files[1].Path != "bar.go" {
		t.Errorf("second file = %q, want bar.go", files[1].Path)
	}
}
