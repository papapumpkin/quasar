package gum

import (
	"context"
	"os/exec"
	"testing"
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
}

func TestGum_Filter_NoItems(t *testing.T) {
	t.Parallel()

	g := &Gum{BinPath: "gum"}
	_, err := g.Filter(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty items")
	}
}

func TestGum_Format_MockBinary(t *testing.T) {
	t.Parallel()

	// Skip if gum is not installed — this test verifies real gum behavior.
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
