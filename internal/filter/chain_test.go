package filter

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mockClaimChecker implements ClaimChecker for testing.
type mockClaimChecker struct {
	owners map[string]string // filepath -> owner task ID
	err    error             // if non-nil, FileOwner returns this error
}

func (m *mockClaimChecker) FileOwner(_ context.Context, fp string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.owners[fp], nil
}

func TestChainRun(t *testing.T) {
	t.Parallel()

	t.Run("AllPass", func(t *testing.T) {
		t.Parallel()
		chain := &Chain{
			Checks: []Check{
				{Name: "check1", Fn: func(_ context.Context, _ string) (string, error) {
					return "", nil
				}},
				{Name: "check2", Fn: func(_ context.Context, _ string) (string, error) {
					return "", nil
				}},
			},
		}

		result, err := chain.Run(context.Background(), "/tmp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Passed {
			t.Error("expected result.Passed to be true")
		}
		if len(result.Checks) != 2 {
			t.Fatalf("expected 2 checks, got %d", len(result.Checks))
		}
		for _, c := range result.Checks {
			if !c.Passed {
				t.Errorf("check %q should have passed", c.Name)
			}
		}
	})

	t.Run("StopsOnFirstFailure", func(t *testing.T) {
		t.Parallel()
		check3Called := false
		chain := &Chain{
			Checks: []Check{
				{Name: "pass", Fn: func(_ context.Context, _ string) (string, error) {
					return "", nil
				}},
				{Name: "fail", Fn: func(_ context.Context, _ string) (string, error) {
					return "something broke", errors.New("check failed")
				}},
				{Name: "skip", Fn: func(_ context.Context, _ string) (string, error) {
					check3Called = true
					return "", nil
				}},
			},
		}

		result, err := chain.Run(context.Background(), "/tmp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Passed {
			t.Error("expected result.Passed to be false")
		}
		if len(result.Checks) != 2 {
			t.Fatalf("expected 2 checks (stopped at failure), got %d", len(result.Checks))
		}
		if result.Checks[0].Name != "pass" || !result.Checks[0].Passed {
			t.Error("first check should be 'pass' and passed")
		}
		if result.Checks[1].Name != "fail" || result.Checks[1].Passed {
			t.Error("second check should be 'fail' and not passed")
		}
		if result.Checks[1].Output != "something broke" {
			t.Errorf("expected failure output, got %q", result.Checks[1].Output)
		}
		if check3Called {
			t.Error("third check should not have been called")
		}
	})

	t.Run("CancelledContext", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // immediately cancelled

		chain := &Chain{
			Checks: []Check{
				{Name: "never", Fn: func(_ context.Context, _ string) (string, error) {
					return "", nil
				}},
			},
		}

		_, err := chain.Run(ctx, "/tmp")
		if err == nil {
			t.Fatal("expected error for cancelled context")
		}
		if !strings.Contains(err.Error(), "filter chain cancelled") {
			t.Errorf("error = %q, want to contain 'filter chain cancelled'", err.Error())
		}
	})

	t.Run("EmptyChain", func(t *testing.T) {
		t.Parallel()
		chain := &Chain{}

		result, err := chain.Run(context.Background(), "/tmp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Passed {
			t.Error("empty chain should pass")
		}
		if len(result.Checks) != 0 {
			t.Errorf("expected 0 checks, got %d", len(result.Checks))
		}
	})

	t.Run("ElapsedTracked", func(t *testing.T) {
		t.Parallel()
		chain := &Chain{
			Checks: []Check{
				{Name: "slow", Fn: func(_ context.Context, _ string) (string, error) {
					time.Sleep(10 * time.Millisecond)
					return "", nil
				}},
			},
		}

		result, err := chain.Run(context.Background(), "/tmp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Checks[0].Elapsed < 10*time.Millisecond {
			t.Errorf("elapsed = %v, want >= 10ms", result.Checks[0].Elapsed)
		}
	})
}

func TestFirstFailure(t *testing.T) {
	t.Parallel()

	t.Run("NoFailure", func(t *testing.T) {
		t.Parallel()
		r := &Result{
			Passed: true,
			Checks: []CheckResult{
				{Name: "a", Passed: true},
				{Name: "b", Passed: true},
			},
		}
		if f := r.FirstFailure(); f != nil {
			t.Errorf("expected nil, got %v", f)
		}
	})

	t.Run("ReturnsFirst", func(t *testing.T) {
		t.Parallel()
		r := &Result{
			Passed: false,
			Checks: []CheckResult{
				{Name: "a", Passed: true},
				{Name: "b", Passed: false, Output: "failed"},
			},
		}
		f := r.FirstFailure()
		if f == nil {
			t.Fatal("expected non-nil failure")
		}
		if f.Name != "b" {
			t.Errorf("expected first failure 'b', got %q", f.Name)
		}
	})
}

func TestClaimsCheck(t *testing.T) {
	t.Parallel()

	t.Run("AllClaimed", func(t *testing.T) {
		t.Parallel()
		checker := &mockClaimChecker{
			owners: map[string]string{
				"file1.go": "task-1",
				"file2.go": "task-1",
			},
		}
		fn := claimsCheck(checker, "task-1", []string{"file1.go", "file2.go"})
		output, err := fn(context.Background(), "/tmp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if output != "" {
			t.Errorf("expected empty output, got %q", output)
		}
	})

	t.Run("UnclaimedFile", func(t *testing.T) {
		t.Parallel()
		checker := &mockClaimChecker{
			owners: map[string]string{
				"file1.go": "task-1",
				"file2.go": "other-task",
			},
		}
		fn := claimsCheck(checker, "task-1", []string{"file1.go", "file2.go"})
		output, err := fn(context.Background(), "/tmp")
		if err == nil {
			t.Fatal("expected error for unclaimed file")
		}
		if !strings.Contains(output, "file2.go") {
			t.Errorf("output should mention the unclaimed file, got %q", output)
		}
		if !strings.Contains(output, "other-task") {
			t.Errorf("output should mention the owning task, got %q", output)
		}
	})

	t.Run("NoModifiedFiles", func(t *testing.T) {
		t.Parallel()
		checker := &mockClaimChecker{}
		fn := claimsCheck(checker, "task-1", nil)
		output, err := fn(context.Background(), "/tmp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if output != "" {
			t.Errorf("expected empty output, got %q", output)
		}
	})

	t.Run("FabricError", func(t *testing.T) {
		t.Parallel()
		checker := &mockClaimChecker{err: errors.New("db error")}
		fn := claimsCheck(checker, "task-1", []string{"file.go"})
		_, err := fn(context.Background(), "/tmp")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "claim check failed") {
			t.Errorf("error = %q, want to contain 'claim check failed'", err.Error())
		}
	})

	t.Run("UnownedFilePasses", func(t *testing.T) {
		t.Parallel()
		// Files with no owner (empty string) are okay — they're unclaimed,
		// not claimed by someone else.
		checker := &mockClaimChecker{
			owners: map[string]string{},
		}
		fn := claimsCheck(checker, "task-1", []string{"new-file.go"})
		output, err := fn(context.Background(), "/tmp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if output != "" {
			t.Errorf("expected empty output, got %q", output)
		}
	})
}

func TestBuildCheck(t *testing.T) {
	t.Parallel()

	t.Run("ValidGoCode", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		// Create a valid Go module and source file.
		writeFile(t, filepath.Join(dir, "go.mod"), "module testmod\n\ngo 1.21\n")
		writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")

		output, err := buildCheck(context.Background(), dir)
		if err != nil {
			t.Fatalf("expected no error for valid code, got: %v\noutput: %s", err, output)
		}
	})

	t.Run("InvalidGoCode", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		writeFile(t, filepath.Join(dir, "go.mod"), "module testmod\n\ngo 1.21\n")
		writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {\n\tundeclaredVar\n}\n")

		output, err := buildCheck(context.Background(), dir)
		if err == nil {
			t.Fatal("expected error for invalid code")
		}
		if output == "" {
			t.Error("expected non-empty output for build failure")
		}
	})
}

func TestVetCheck(t *testing.T) {
	t.Parallel()

	t.Run("CleanCode", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		writeFile(t, filepath.Join(dir, "go.mod"), "module testmod\n\ngo 1.21\n")
		writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hello\") }\n")

		output, err := vetCheck(context.Background(), dir)
		if err != nil {
			t.Fatalf("expected no error, got: %v\noutput: %s", err, output)
		}
	})
}

func TestTestCheck(t *testing.T) {
	t.Parallel()

	t.Run("PassingTests", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		writeFile(t, filepath.Join(dir, "go.mod"), "module testmod\n\ngo 1.21\n")
		writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc Add(a, b int) int { return a + b }\n\nfunc main() {}\n")
		writeFile(t, filepath.Join(dir, "main_test.go"), `package main

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Error("expected 3")
	}
}
`)

		output, err := testCheck(context.Background(), dir)
		if err != nil {
			t.Fatalf("expected no error, got: %v\noutput: %s", err, output)
		}
	})

	t.Run("FailingTests", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		writeFile(t, filepath.Join(dir, "go.mod"), "module testmod\n\ngo 1.21\n")
		writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc Add(a, b int) int { return a + b }\n\nfunc main() {}\n")
		writeFile(t, filepath.Join(dir, "main_test.go"), `package main

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 99 {
		t.Error("expected 99")
	}
}
`)

		output, err := testCheck(context.Background(), dir)
		if err == nil {
			t.Fatal("expected error for failing tests")
		}
		if !strings.Contains(output, "FAIL") {
			t.Errorf("expected FAIL in output, got %q", output)
		}
	})
}

func TestLintCheck(t *testing.T) {
	t.Parallel()

	// lintCheck skips when golangci-lint is not on PATH.
	// We can't guarantee it's installed, so we test the skip behavior.
	t.Run("SkipsIfNotAvailable", func(t *testing.T) {
		t.Parallel()

		output, err := lintCheck(context.Background(), t.TempDir())
		_, lookErr := exec.LookPath("golangci-lint")
		if lookErr != nil {
			// golangci-lint is not installed — skip path should succeed silently.
			if err != nil {
				t.Errorf("expected nil error when golangci-lint absent, got %v", err)
			}
			if output != "" {
				t.Errorf("expected empty output when golangci-lint absent, got %q", output)
			}
		}
		// If golangci-lint IS installed, the call may fail on the empty temp dir,
		// which is fine — we only assert the skip path above.
	})
}

func TestDefaultChain(t *testing.T) {
	t.Parallel()

	t.Run("WithoutFabric", func(t *testing.T) {
		t.Parallel()
		chain := DefaultChain(nil, "", nil)
		// Should have build, vet, lint, test — no claims.
		if len(chain.Checks) != 4 {
			t.Errorf("expected 4 checks without fabric, got %d", len(chain.Checks))
		}
		names := make([]string, len(chain.Checks))
		for i, c := range chain.Checks {
			names[i] = c.Name
		}
		expected := []string{"build", "vet", "lint", "test"}
		for i, want := range expected {
			if i >= len(names) || names[i] != want {
				t.Errorf("check[%d] = %q, want %q", i, names[i], want)
			}
		}
	})

	t.Run("WithFabric", func(t *testing.T) {
		t.Parallel()
		checker := &mockClaimChecker{}
		chain := DefaultChain(checker, "task-1", []string{"file.go"})
		// Should have build, vet, lint, test, claims.
		if len(chain.Checks) != 5 {
			t.Errorf("expected 5 checks with fabric, got %d", len(chain.Checks))
		}
		if chain.Checks[4].Name != "claims" {
			t.Errorf("last check = %q, want 'claims'", chain.Checks[4].Name)
		}
	})
}

func TestRunCommand(t *testing.T) {
	t.Parallel()

	t.Run("SuccessfulCommand", func(t *testing.T) {
		t.Parallel()
		output, err := runCommand(context.Background(), t.TempDir(), "echo", "hello")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(output, "hello") {
			t.Errorf("output = %q, want to contain 'hello'", output)
		}
	})

	t.Run("FailingCommand", func(t *testing.T) {
		t.Parallel()
		_, err := runCommand(context.Background(), t.TempDir(), "false")
		if err == nil {
			t.Fatal("expected error for failing command")
		}
	})
}

// writeFile is a test helper that creates a file with the given content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}
