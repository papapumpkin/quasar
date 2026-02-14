package nebula

import "errors"

var (
	ErrNoManifest      = errors.New("nebula.toml not found in nebula directory")
	ErrDuplicateID     = errors.New("duplicate task ID")
	ErrDependencyCycle = errors.New("dependency cycle detected")
	ErrUnknownDep      = errors.New("task depends on unknown task ID")
	ErrMissingField    = errors.New("required field missing")
	ErrUnmetDependency = errors.New("unmet external dependency")
)

// ValidationError records a validation problem with source context.
type ValidationError struct {
	TaskID     string
	SourceFile string
	Field      string
	Err        error
}

func (e *ValidationError) Error() string {
	if e.TaskID != "" {
		return e.SourceFile + ": task " + e.TaskID + ": " + e.Err.Error()
	}
	return e.SourceFile + ": " + e.Err.Error()
}

func (e *ValidationError) Unwrap() error {
	return e.Err
}
