// Package filter provides deterministic pre-reviewer checks that validate
// coder output before invoking the reviewer agent. Checks like build, vet,
// lint, test, and claim validation catch issues a compiler or test suite can
// find in milliseconds, saving full reviewer inference.
package filter

import (
	"context"
	"time"
)

// Filter runs deterministic checks on coder output.
// Returns the result of all checks, or an error if the filter
// infrastructure itself fails (not check failures, which are captured
// in Result).
type Filter interface {
	Run(ctx context.Context, workDir string) (*Result, error)
}

// Result contains the outcome of filter execution.
type Result struct {
	Passed bool          // true if all checks passed
	Checks []CheckResult // individual check outcomes
}

// CheckResult is the outcome of a single check.
type CheckResult struct {
	Name    string        // "build", "vet", "lint", "test", "claims"
	Passed  bool          // true if this check passed
	Output  string        // stdout+stderr on failure
	Elapsed time.Duration // wall-clock time for this check
}

// FirstFailure returns the first failing check, or nil if all passed.
func (r *Result) FirstFailure() *CheckResult {
	for i := range r.Checks {
		if !r.Checks[i].Passed {
			return &r.Checks[i]
		}
	}
	return nil
}
