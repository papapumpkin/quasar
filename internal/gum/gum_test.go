package gum

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/dialog"
	"github.com/papapumpkin/quasar/internal/ui"
)

func TestNew_NotFound(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.
	t.Setenv("PATH", "/nonexistent-path-for-gum-test")

	_, err := New()
	if err == nil {
		t.Fatal("expected error when gum not in PATH")
	}
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestAvailable(t *testing.T) {
	t.Parallel()

	// Just verify it doesn't panic. The result depends on whether
	// gum is installed in the test environment.
	_ = Available()
}

func TestGum_Validate_EmptyPath(t *testing.T) {
	t.Parallel()

	g := &Gum{BinPath: ""}
	err := g.Validate()
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for empty BinPath, got %v", err)
	}
}

func TestGum_Validate_NonexistentPath(t *testing.T) {
	t.Parallel()

	g := &Gum{BinPath: "/nonexistent/gum"}
	err := g.Validate()
	if err == nil {
		t.Fatal("expected error for nonexistent binary path")
	}
}

func TestGum_Choose_NoOptions(t *testing.T) {
	t.Parallel()

	g := &Gum{BinPath: "gum"}
	_, err := g.Choose(context.Background(), "pick one", nil)
	if err == nil {
		t.Fatal("expected error for empty options")
	}
	if !strings.Contains(err.Error(), "no options") {
		t.Errorf("expected 'no options' in error, got %q", err)
	}
}

func TestGum_Filter_NoItems(t *testing.T) {
	t.Parallel()

	g := &Gum{BinPath: "gum"}
	_, err := g.Filter(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty items")
	}
	if !strings.Contains(err.Error(), "no items") {
		t.Errorf("expected 'no items' in error, got %q", err)
	}
}

func TestGum_Format_Integration(t *testing.T) {
	t.Parallel()

	path, err := exec.LookPath("gum")
	if err != nil {
		t.Skip("gum not installed, skipping integration test")
	}

	g := &Gum{BinPath: path}
	out, err := g.Format(context.Background(), "# Hello\n\nWorld")
	if err != nil {
		t.Fatalf("Format() error: %v", err)
	}
	if out == "" {
		t.Error("expected non-empty formatted output")
	}
}

func TestGum_Validate_RealBinary(t *testing.T) {
	t.Parallel()

	path, err := exec.LookPath("gum")
	if err != nil {
		t.Skip("gum not installed, skipping validation test")
	}

	g := &Gum{BinPath: path}
	if err := g.Validate(); err != nil {
		t.Errorf("Validate() should pass for real gum binary: %v", err)
	}
}

// --- BuildHailMarkdown tests ---

func TestBuildHailMarkdown(t *testing.T) {
	t.Parallel()

	t.Run("basic hail", func(t *testing.T) {
		t.Parallel()
		hail := ui.HailInfo{
			ID:      "hail-001",
			Kind:    "blocker",
			Summary: "Missing dependency",
			Detail:  "Cannot find module X",
		}
		md := BuildHailMarkdown(hail)
		if !strings.Contains(md, "Missing dependency") {
			t.Errorf("expected summary in markdown, got %q", md)
		}
		if !strings.Contains(md, "blocker") {
			t.Errorf("expected kind in markdown, got %q", md)
		}
		if !strings.Contains(md, "Cannot find module X") {
			t.Errorf("expected detail in markdown, got %q", md)
		}
	})

	t.Run("with source role and cycle", func(t *testing.T) {
		t.Parallel()
		hail := ui.HailInfo{
			ID:         "hail-002",
			Kind:       "decision_needed",
			Summary:    "Choose an approach",
			SourceRole: "reviewer",
			Cycle:      3,
		}
		md := BuildHailMarkdown(hail)
		if !strings.Contains(md, "reviewer") {
			t.Errorf("expected source role in markdown, got %q", md)
		}
		if !strings.Contains(md, "cycle 3") {
			t.Errorf("expected cycle in markdown, got %q", md)
		}
	})

	t.Run("empty hail", func(t *testing.T) {
		t.Parallel()
		hail := ui.HailInfo{ID: "hail-003"}
		md := BuildHailMarkdown(hail)
		if !strings.Contains(md, "HAIL") {
			t.Errorf("expected HAIL header in markdown, got %q", md)
		}
	})

	t.Run("no kind omits kind line", func(t *testing.T) {
		t.Parallel()
		hail := ui.HailInfo{ID: "hail-004", Summary: "Test"}
		md := BuildHailMarkdown(hail)
		if strings.Contains(md, "Kind:") {
			t.Errorf("expected no Kind line when kind is empty, got %q", md)
		}
	})
}

// --- GumHailCmd tests ---

func TestGumHailCmd_WithOptions(t *testing.T) {
	t.Parallel()

	hail := ui.HailInfo{
		ID:      "hail-001",
		Kind:    "decision_needed",
		Summary: "Pick one",
		Options: []string{"retry", "skip", "fail"},
	}

	cmd := GumHailCmd(context.Background(), "/usr/bin/gum", hail)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	// The command should be sh -c <script>.
	if len(cmd.Args) < 3 {
		t.Fatalf("expected sh -c <script>, got %v", cmd.Args)
	}
	script := cmd.Args[2]
	if !strings.Contains(script, "choose") {
		t.Errorf("expected 'choose' in script for options hail, got %q", script)
	}
	for _, opt := range hail.Options {
		if !strings.Contains(script, opt) {
			t.Errorf("expected option %q in script, got %q", opt, script)
		}
	}
}

func TestGumHailCmd_WithoutOptions(t *testing.T) {
	t.Parallel()

	hail := ui.HailInfo{
		ID:      "hail-002",
		Kind:    "ambiguity",
		Summary: "Unclear requirement",
		Detail:  "What does this mean?",
	}

	cmd := GumHailCmd(context.Background(), "/usr/local/bin/gum", hail)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	script := cmd.Args[2]
	if !strings.Contains(script, "write") {
		t.Errorf("expected 'write' in script for no-options hail, got %q", script)
	}
}

func TestGumHailCmd_ShellEscaping(t *testing.T) {
	t.Parallel()

	hail := ui.HailInfo{
		ID:      "hail-003",
		Summary: "It's a problem",
		Detail:  "Can't resolve this",
		Options: []string{"it's fine", "skip"},
	}

	cmd := GumHailCmd(context.Background(), "/usr/bin/gum", hail)
	script := cmd.Args[2]

	// Single quotes in content should be escaped for shell safety.
	// Raw "It's" should become "It'\''s" in the script.
	if strings.Count(script, "'\\''") < 2 {
		t.Errorf("expected escaped single quotes in script for content with apostrophes")
	}
}

func TestGumHailCmd_UsesContextPath(t *testing.T) {
	t.Parallel()

	hail := ui.HailInfo{
		ID:      "hail-004",
		Summary: "Test",
	}

	customPath := "/custom/path/to/gum"
	cmd := GumHailCmd(context.Background(), customPath, hail)
	script := cmd.Args[2]

	if !strings.Contains(script, customPath) {
		t.Errorf("expected custom gum path %q in script, got %q", customPath, script)
	}
}

func TestGumHailCmd_PrintfNotEcho(t *testing.T) {
	t.Parallel()

	hail := ui.HailInfo{
		ID:      "hail-005",
		Summary: "Test",
		Detail:  "Detail with \\n backslash",
	}

	cmd := GumHailCmd(context.Background(), "/usr/bin/gum", hail)
	script := cmd.Args[2]

	// Should use printf '%s' not echo (echo interprets escape sequences).
	if strings.Contains(script, "echo '") {
		t.Errorf("expected printf not echo in script, got %q", script)
	}
	if !strings.Contains(script, "printf") {
		t.Errorf("expected printf in script, got %q", script)
	}
}

// --- GumDialogCmd tests ---

func TestGumDialogCmd_WithOptions(t *testing.T) {
	t.Parallel()

	req := dialog.Request{
		Title:   "Choose action",
		Kind:    "escalation",
		PhaseID: "phase-1",
		Options: []string{"approve", "reject"},
	}

	cmd := GumDialogCmd(context.Background(), "/usr/bin/gum", req)
	script := cmd.Args[2]
	if !strings.Contains(script, "choose") {
		t.Errorf("expected 'choose' for options dialog, got %q", script)
	}
	if !strings.Contains(script, "approve") {
		t.Errorf("expected option in script, got %q", script)
	}
}

func TestGumDialogCmd_FreeText(t *testing.T) {
	t.Parallel()

	req := dialog.Request{
		Title:   "Question",
		Context: "Some context markdown",
	}

	cmd := GumDialogCmd(context.Background(), "/usr/bin/gum", req)
	script := cmd.Args[2]
	if !strings.Contains(script, "write") {
		t.Errorf("expected 'write' for free-text dialog, got %q", script)
	}
}

func TestGumDialogCmd_RendersContext(t *testing.T) {
	t.Parallel()

	req := dialog.Request{
		Title:   "Review",
		Context: "## Problem\n\nThis needs review.",
		PhaseID: "phase-2",
	}

	cmd := GumDialogCmd(context.Background(), "/usr/bin/gum", req)
	script := cmd.Args[2]
	if !strings.Contains(script, "format") {
		t.Errorf("expected 'format' for context rendering, got %q", script)
	}
	if !strings.Contains(script, "phase-2") {
		t.Errorf("expected phase ID in script, got %q", script)
	}
}

// --- DialogOpener compile-time interface check ---

func TestDialogOpener_ImplementsInterface(t *testing.T) {
	t.Parallel()

	g := &Gum{BinPath: "/nonexistent/gum"}
	opener := NewDialogOpener(g)
	if opener == nil {
		t.Fatal("expected non-nil DialogOpener")
	}
	// Compile-time check is done by the var _ declaration in dialog.go.
}

// --- HailResolver construction ---

func TestHailResolver_New(t *testing.T) {
	t.Parallel()

	g := &Gum{BinPath: "/nonexistent/gum"}
	resolver := NewHailResolver(g)
	if resolver == nil {
		t.Fatal("expected non-nil HailResolver")
	}
}
