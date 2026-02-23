package filter

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ClaimChecker checks file ownership against the fabric. Defined here
// (where consumed) per project convention rather than in the fabric package.
type ClaimChecker interface {
	// FileOwner returns the phase ID that owns the given file path,
	// or empty string if unclaimed.
	FileOwner(ctx context.Context, filepath string) (string, error)
}

// Check is a single named check function in the filter chain.
type Check struct {
	Name string
	Fn   func(ctx context.Context, workDir string) (output string, err error)
}

// Chain runs checks sequentially, stopping on first failure.
type Chain struct {
	Checks []Check
}

// Run executes each check in sequence. It stops on the first failure and
// returns a Result with Passed=false. If all checks pass, Passed=true.
// A non-nil error is only returned for infrastructure failures (e.g. context
// cancelled), not for check failures which are captured in CheckResult.
func (c *Chain) Run(ctx context.Context, workDir string) (*Result, error) {
	result := &Result{Passed: true}

	for _, check := range c.Checks {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("filter chain cancelled: %w", err)
		}

		start := time.Now()
		output, err := check.Fn(ctx, workDir)
		elapsed := time.Since(start)

		if err != nil {
			// Check failed — record it and stop.
			result.Passed = false
			result.Checks = append(result.Checks, CheckResult{
				Name:    check.Name,
				Passed:  false,
				Output:  output,
				Elapsed: elapsed,
			})
			return result, nil
		}

		result.Checks = append(result.Checks, CheckResult{
			Name:    check.Name,
			Passed:  true,
			Output:  output,
			Elapsed: elapsed,
		})
	}

	return result, nil
}

// DefaultChain returns the standard pre-reviewer filter chain:
// build, vet, lint (if available), test, claims (if fabric present).
func DefaultChain(fabric ClaimChecker, taskID string, modifiedFiles []string) *Chain {
	checks := []Check{
		{Name: "build", Fn: buildCheck},
		{Name: "vet", Fn: vetCheck},
		{Name: "lint", Fn: lintCheck},
		{Name: "test", Fn: testCheck},
	}

	if fabric != nil {
		checks = append(checks, Check{
			Name: "claims",
			Fn:   claimsCheck(fabric, taskID, modifiedFiles),
		})
	}

	return &Chain{Checks: checks}
}

// buildCheck runs `go build ./...`.
func buildCheck(ctx context.Context, workDir string) (string, error) {
	return runCommand(ctx, workDir, "go", "build", "./...")
}

// vetCheck runs `go vet ./...`.
func vetCheck(ctx context.Context, workDir string) (string, error) {
	return runCommand(ctx, workDir, "go", "vet", "./...")
}

// lintCheck runs `golangci-lint run` if available on PATH. If the binary
// is not found, the check passes silently (not an error).
func lintCheck(ctx context.Context, workDir string) (string, error) {
	_, err := exec.LookPath("golangci-lint")
	if err != nil {
		// Not available — skip silently.
		return "", nil
	}
	return runCommand(ctx, workDir, "golangci-lint", "run")
}

// testCheck runs `go test ./...`.
func testCheck(ctx context.Context, workDir string) (string, error) {
	return runCommand(ctx, workDir, "go", "test", "./...")
}

// claimsCheck returns a check function that validates every modified file
// is claimed by the given task on the fabric.
func claimsCheck(fabric ClaimChecker, taskID string, modifiedFiles []string) func(ctx context.Context, workDir string) (string, error) {
	return func(ctx context.Context, _ string) (string, error) {
		if len(modifiedFiles) == 0 {
			return "", nil
		}

		var unclaimed []string
		for _, f := range modifiedFiles {
			owner, err := fabric.FileOwner(ctx, f)
			if err != nil {
				return fmt.Sprintf("failed to check ownership for %s: %v", f, err), fmt.Errorf("claim check failed: %w", err)
			}
			if owner != "" && owner != taskID {
				unclaimed = append(unclaimed, fmt.Sprintf("  %s (owned by %s)", f, owner))
			}
		}

		if len(unclaimed) > 0 {
			output := fmt.Sprintf("Modified files not claimed by task %s:\n%s", taskID, strings.Join(unclaimed, "\n"))
			return output, fmt.Errorf("claim validation failed")
		}

		return "", nil
	}
}

// runCommand executes a command and returns combined stdout+stderr output.
// Returns a nil error if the command exits 0, or a non-nil error with the
// output text for non-zero exits.
func runCommand(ctx context.Context, workDir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workDir

	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	err := cmd.Run()
	output := strings.TrimSpace(combined.String())

	if err != nil {
		if output == "" {
			output = err.Error()
		}
		return output, err
	}

	return output, nil
}
