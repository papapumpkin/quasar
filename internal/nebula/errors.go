package nebula

import "errors"

// Sentinel errors for nebula validation and dependency checking.
var (
	// ErrNoManifest indicates no nebula.toml was found in the nebula directory.
	ErrNoManifest = errors.New("nebula.toml not found in nebula directory")
	// ErrDuplicateID indicates two or more phases share the same ID.
	ErrDuplicateID = errors.New("duplicate phase ID")
	// ErrDependencyCycle indicates a circular dependency among phases.
	ErrDependencyCycle = errors.New("dependency cycle detected")
	// ErrUnknownDep indicates a phase depends on a phase ID that does not exist.
	ErrUnknownDep = errors.New("phase depends on unknown phase ID")
	// ErrMissingField indicates a required field (e.g. id, title) is empty.
	ErrMissingField = errors.New("required field missing")
	// ErrUnmetDependency indicates an external dependency (bead or nebula) is not satisfied.
	ErrUnmetDependency = errors.New("unmet external dependency")
	// ErrManualStop indicates the user requested a graceful stop via a STOP file.
	ErrManualStop = errors.New("nebula stopped by user")
	// ErrInvalidGate indicates an unrecognized gate mode value.
	ErrInvalidGate = errors.New("invalid gate mode")
	// ErrPlanRejected indicates the human rejected the execution plan before any phases ran.
	ErrPlanRejected = errors.New("execution plan rejected")
	// ErrScopeOverlap indicates two or more phases declare overlapping file ownership scopes.
	ErrScopeOverlap = errors.New("scope overlap")
	// ErrPhaseAlreadyStarted indicates a reverse-dep target has already begun executing.
	ErrPhaseAlreadyStarted = errors.New("phase already started")
	// ErrPlanHasErrors indicates the execution plan contains error-severity risks.
	ErrPlanHasErrors = errors.New("execution plan has error-severity risks")
)

// ValidationCategory classifies a validation error for programmatic handling.
type ValidationCategory string

const (
	// ValCatMissingField indicates a required field is empty.
	ValCatMissingField ValidationCategory = "missing_field"
	// ValCatDuplicateID indicates two or more phases share the same ID.
	ValCatDuplicateID ValidationCategory = "duplicate_id"
	// ValCatUnknownDep indicates a dependency references a non-existent phase.
	ValCatUnknownDep ValidationCategory = "unknown_dep"
	// ValCatCycle indicates a circular dependency among phases.
	ValCatCycle ValidationCategory = "cycle"
	// ValCatInvalidGate indicates an unrecognized gate mode value.
	ValCatInvalidGate ValidationCategory = "invalid_gate"
	// ValCatScopeOverlap indicates overlapping file ownership scopes.
	ValCatScopeOverlap ValidationCategory = "scope_overlap"
	// ValCatBoundsViolation indicates a numeric field is out of valid range.
	ValCatBoundsViolation ValidationCategory = "bounds_violation"
)

// ValidationError records a validation problem with source context.
type ValidationError struct {
	Category   ValidationCategory // Machine-readable category for programmatic handling
	PhaseID    string
	SourceFile string
	Field      string
	Err        error
}

// Error returns a human-readable string including source file and phase context.
func (e *ValidationError) Error() string {
	if e.PhaseID != "" {
		return e.SourceFile + ": phase " + e.PhaseID + ": " + e.Err.Error()
	}
	return e.SourceFile + ": " + e.Err.Error()
}

// Unwrap returns the underlying error for use with errors.Is/As.
func (e *ValidationError) Unwrap() error {
	return e.Err
}
