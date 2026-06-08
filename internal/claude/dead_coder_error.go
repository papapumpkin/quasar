package claude

import "fmt"

// DeadCoderError is returned by the Invoker when a healthcheck terminates a
// claude subprocess that was declared Dead (stalled or thrashing) before it
// exited on its own. It is distinct from an ordinary non-zero exit: the
// supervisor catches it to mark the cycle terminated_health (rather than
// failed) and hand the partial worktree to the reviewer for judgement.
type DeadCoderError struct {
	// Reason is the human-readable explanation of why the coder was killed
	// (e.g. "wall-clock cap exceeded" or "two signals red: write_idle, token_rate").
	Reason string

	// Signals lists the signal names that were red at termination. For a
	// wall-clock kill this is just {"wall_clock"}.
	Signals []string

	// Elapsed is how long the subprocess ran before it was terminated.
	Elapsed string

	// Workdir is the live worktree the killed coder was operating in. The
	// partial work persists there in-place (this runtime does not snapshot to a
	// separate <state>.partial tree — the sequential loop reviews the live tree
	// directly), so the reviewer judges whether that partial work is shippable.
	// A frozen snapshot only becomes necessary once concurrent coders can mutate
	// the tree before review (the entanglements phase).
	Workdir string
}

// Error implements the error interface.
func (e *DeadCoderError) Error() string {
	return fmt.Sprintf("coder terminated by healthcheck after %s: %s", e.Elapsed, e.Reason)
}
