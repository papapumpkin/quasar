package loop

import "errors"

// Sentinel errors returned by the coder-reviewer loop.
var (
	// ErrMaxCycles is returned when the loop exhausts all allowed review cycles.
	ErrMaxCycles = errors.New("maximum review cycles reached")
	// ErrBudgetExceeded is returned when cumulative cost reaches the budget limit.
	ErrBudgetExceeded = errors.New("budget exceeded")
)
