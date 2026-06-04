package github

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// TestDefaultRunGHSmoke is the one test that invokes the real gh binary. It is
// skipped in -short mode and when gh is not installed, so the default
// `go test ./...` run never shells out to gh.
func TestDefaultRunGHSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real gh invocation in -short mode")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh not installed; skipping smoke test")
	}

	out, err := defaultRunGH("")(context.Background(), "--version")
	if err != nil {
		t.Fatalf("gh --version failed: %v", err)
	}
	if !strings.Contains(string(out), "gh version") {
		t.Errorf("gh --version output = %q, want it to contain 'gh version'", out)
	}
}

// TestDefaultRunGHClassifiesExit verifies that a failing gh command yields a
// *ghError carrying a non-zero exit code, which classifyGHError relies on.
func TestDefaultRunGHClassifiesExit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real gh invocation in -short mode")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh not installed; skipping smoke test")
	}

	_, err := defaultRunGH("")(context.Background(), "definitely-not-a-real-subcommand")
	if err == nil {
		t.Fatal("expected error for bogus gh subcommand, got nil")
	}
	var ge *ghError
	if !errors.As(err, &ge) {
		t.Fatalf("error = %v, want *ghError", err)
	}
	if ge.ExitCode == 0 {
		t.Errorf("ghError.ExitCode = 0, want non-zero")
	}
}
