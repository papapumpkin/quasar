package nebula

import "errors"

// Sentinel errors for nebula validation and dependency checking.
var (
	// ErrNoManifest indicates no nebula.toml was found in the nebula directory.
	ErrNoManifest = errors.New("nebula.toml not found in nebula directory")
	// ErrDuplicateID indicates two or more tasks share the same ID.
	ErrDuplicateID = errors.New("duplicate task ID")
	// ErrDependencyCycle indicates a circular dependency among tasks.
	ErrDependencyCycle = errors.New("dependency cycle detected")
	// ErrUnknownDep indicates a task depends on a task ID that does not exist.
	ErrUnknownDep = errors.New("task depends on unknown task ID")
	// ErrMissingField indicates a required field (e.g. id, title) is empty.
	ErrMissingField = errors.New("required field missing")
	// ErrUnmetDependency indicates an external dependency (bead or nebula) is not satisfied.
	ErrUnmetDependency = errors.New("unmet external dependency")
)

// ValidationError records a validation problem with source context.
type ValidationError struct {
	TaskID     string
	SourceFile string
	Field      string
	Err        error
}

// Error returns a human-readable string including source file and task context.
func (e *ValidationError) Error() string {
	if e.TaskID != "" {
		return e.SourceFile + ": task " + e.TaskID + ": " + e.Err.Error()
	}
	return e.SourceFile + ": " + e.Err.Error()
}

// Unwrap returns the underlying error for use with errors.Is/As.
func (e *ValidationError) Unwrap() error {
	return e.Err
}
