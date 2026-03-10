package gum

import (
	"strings"
	"testing"
)

func TestBuildContextMarkdown_Full(t *testing.T) {
	t.Parallel()

	ctx := PhaseContext{
		PhaseID:        "unified-diff",
		PhaseTitle:     "Implement unified diff view",
		Cycle:          2,
		MaxCycles:      5,
		ActiveAgent:    "coder",
		LastFeedback:   "3 issues found: missing error handling in diffview.go",
		FileClaims:     []string{"internal/tui/diffview.go", "internal/tui/model.go"},
		RecentActivity: "reading internal/tui/diffview.go",
	}

	md := buildContextMarkdown(ctx)

	checks := []struct {
		name     string
		contains string
	}{
		{"phase ID", "unified-diff"},
		{"title", "Implement unified diff view"},
		{"cycle info", "2/5"},
		{"active agent", "coder"},
		{"feedback header", "Last reviewer feedback"},
		{"feedback content", "3 issues found"},
		{"files header", "Files claimed"},
		{"file path", "internal/tui/diffview.go"},
		{"recent activity", "reading internal/tui/diffview.go"},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(md, c.contains) {
				t.Errorf("buildContextMarkdown() missing %q\nGot:\n%s", c.contains, md)
			}
		})
	}
}

func TestBuildContextMarkdown_Minimal(t *testing.T) {
	t.Parallel()

	ctx := PhaseContext{
		PhaseID: "simple-task",
	}

	md := buildContextMarkdown(ctx)

	if !strings.Contains(md, "simple-task") {
		t.Error("expected phase ID in minimal context")
	}
	// Should not contain sections that have no data.
	if strings.Contains(md, "Last reviewer feedback") {
		t.Error("unexpected feedback section in minimal context")
	}
	if strings.Contains(md, "Files claimed") {
		t.Error("unexpected files section in minimal context")
	}
	if strings.Contains(md, "Recent:") {
		t.Error("unexpected recent activity in minimal context")
	}
}

func TestBuildContextMarkdown_CycleWithoutMax(t *testing.T) {
	t.Parallel()

	ctx := PhaseContext{
		PhaseID: "phase-1",
		Cycle:   3,
	}

	md := buildContextMarkdown(ctx)

	if !strings.Contains(md, "**Cycle:** 3") {
		t.Errorf("expected cycle without max, got:\n%s", md)
	}
	if strings.Contains(md, "3/") {
		t.Error("should not show max cycles denominator when MaxCycles is 0")
	}
}

func TestGumGuidanceWriteCmd_CreatesValidShellScript(t *testing.T) {
	t.Parallel()

	ctx := PhaseContext{
		PhaseID:    "test-phase",
		PhaseTitle: "Test Title",
		Cycle:      1,
		MaxCycles:  5,
	}

	cmd := GumGuidanceWriteCmd("/usr/local/bin/gum", ctx)

	if cmd.Path != "/bin/sh" && !strings.HasSuffix(cmd.Path, "/sh") {
		t.Errorf("expected sh command, got %q", cmd.Path)
	}
	if len(cmd.Args) < 3 {
		t.Fatal("expected at least 3 args (sh -c script)")
	}

	script := cmd.Args[2]
	if !strings.Contains(script, "gum format") {
		t.Error("script should contain gum format for context display")
	}
	if !strings.Contains(script, "gum write") {
		t.Error("script should contain gum write for multi-line input")
	}
	if !strings.Contains(script, "Send guidance to agent") {
		t.Error("script should contain guidance header")
	}
}

func TestGumGuidanceInputCmd_CreatesValidShellScript(t *testing.T) {
	t.Parallel()

	ctx := PhaseContext{
		PhaseID: "quick-phase",
	}

	cmd := GumGuidanceInputCmd("/usr/local/bin/gum", ctx)

	if len(cmd.Args) < 3 {
		t.Fatal("expected at least 3 args (sh -c script)")
	}

	script := cmd.Args[2]
	if !strings.Contains(script, "gum format") {
		t.Error("script should contain gum format for context display")
	}
	if !strings.Contains(script, "gum input") {
		t.Error("script should contain gum input for single-line input")
	}
	if !strings.Contains(script, "Quick guidance") {
		t.Error("script should contain quick guidance header")
	}
}

func TestBuildContextMarkdown_ShellQuoteEscaping(t *testing.T) {
	t.Parallel()

	ctx := PhaseContext{
		PhaseID:      "quote-test",
		PhaseTitle:   "It's a test",
		LastFeedback: "reviewer's note: check the 'config' file",
	}

	md := buildContextMarkdown(ctx)

	// The markdown itself should contain the raw text.
	if !strings.Contains(md, "It's a test") {
		t.Error("expected title with apostrophe")
	}
	if !strings.Contains(md, "reviewer's note") {
		t.Error("expected feedback with apostrophe")
	}
}
