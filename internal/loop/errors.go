package loop

import "errors"

var (
	ErrMaxCycles      = errors.New("maximum review cycles reached")
	ErrBudgetExceeded = errors.New("budget exceeded")
)
