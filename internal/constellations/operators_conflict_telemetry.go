package constellations

import (
	"context"
	"fmt"

	"github.com/papapumpkin/quasar/internal/telemetry"
)

// opEmitConflictTelemetryName is the registered name of the terminal telemetry
// node of the merge-conflict-resolve constellation.
const opEmitConflictTelemetryName = "emit_conflict_telemetry"

// opEmitConflictTelemetry appends one resolution-outcome row to the conflict
// log, so `quasar conflicts report` can fold it into the resolution-rate / cost
// / latency summary. It is the terminal-path telemetry node of the
// merge-conflict-resolve constellation: every resolution run (whether it commits
// or escalates) passes through it exactly once, after the cycle loop has settled.
//
// Cycles is read from the authoritative run State, not an input, so it reflects
// the back-edge count regardless of how the row's other fields were threaded.
// latency_ms / cost_usd are sourced by the firing supervisor and omitted until
// that wiring lands (see merge-conflict-resolve.toml). A runtime with no conflict
// log injected makes Record a no-op, so the node never fails a run.
//
// Output: {"recorded": <bool>} — true when a log was wired and the row written.
func opEmitConflictTelemetry(ctx context.Context, rt *Runtime, st *State, args map[string]any) (map[string]any, error) {
	ev := telemetry.ConflictResolutionEvent{
		SrcRun:       stringArg(args, "src_run_id"),
		DstRun:       stringArg(args, "dst_run_id"),
		Mode:         stringArg(args, "mode"),
		Cycles:       st.Cycle,
		Status:       stringArg(args, "status"),
		FilesChanged: intArg(args, "files_changed"),
		Files:        toStringSlice(args["files"]),
	}
	if err := rt.conflictLog.Record(ctx, ev); err != nil {
		return nil, fmt.Errorf("emit_conflict_telemetry: %w", err)
	}
	return map[string]any{"recorded": rt.conflictLog != nil}, nil
}

// intArg coerces a node input into an int, tolerating the int/int64/float64 a
// TOML state round-trip produces from a builtin's numeric output (e.g. the
// decision operator's files_changed crosses a Marshal/Unmarshal boundary before
// the telemetry node reads it). Non-numeric or absent values yield 0.
func intArg(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
