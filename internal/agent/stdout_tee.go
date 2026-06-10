package agent

import (
	"context"
	"io"
)

// stdoutTeeKey is the unexported context key under which a best-effort stdout
// tee writer is carried. It lives in package agent because both the runtime
// (which sets it) and the claude invoker (which reads it) already import agent,
// so no new import-layering edge is introduced.
type stdoutTeeKey struct{}

// WithStdoutTee returns ctx carrying w, a best-effort sink the invoker also
// writes subprocess stdout to (e.g. a per-run tail log the cockpit streams to
// the browser). A nil w is ignored, returning ctx unchanged. The tee is never
// authoritative: the invoker's in-memory buffer remains the single source of
// truth for parsing the result, so a tee that is slow or fails must not affect
// the run.
func WithStdoutTee(ctx context.Context, w io.Writer) context.Context {
	if w == nil {
		return ctx
	}
	return context.WithValue(ctx, stdoutTeeKey{}, w)
}

// StdoutTee returns the tee writer set by WithStdoutTee, or nil when none is
// set. Callers must treat a non-nil writer as best-effort.
func StdoutTee(ctx context.Context) io.Writer {
	w, _ := ctx.Value(stdoutTeeKey{}).(io.Writer)
	return w
}
