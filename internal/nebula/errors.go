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
)

// ValidationError records a validation problem with source context.
type ValidationError struct {
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
