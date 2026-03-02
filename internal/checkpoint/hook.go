// Package checkpoint provides checkpoint persistence for the coder-reviewer loop.
package checkpoint

import (
	"context"
	"fmt"
	"os"

	"github.com/papapumpkin/quasar/internal/loop"
)

// CheckpointHook writes a checkpoint file on significant loop events.
// It satisfies the loop.Hook interface. Checkpoint write errors are logged
// to stderr but do not halt the loop (non-fatal).
type CheckpointHook struct {
	Dir        string                      // directory to write checkpoint files
	PhaseID    string                      // nebula phase ID (may be empty for standalone)
	NebulaName string                      // nebula name (may be empty for standalone)
	GitSHAFunc func() string              // returns current HEAD SHA
	StateFunc  func() *loop.CycleState    // returns current loop cycle state
}

// OnEvent handles a loop lifecycle event. It writes a checkpoint on
// EventReviewComplete, EventTaskSuccess, and EventTaskFailed. All other
// event kinds are ignored. Errors are logged to stderr and do not propagate.
func (h *CheckpointHook) OnEvent(ctx context.Context, event loop.Event) {
	switch event.Kind {
	case loop.EventReviewComplete, loop.EventTaskSuccess, loop.EventTaskFailed:
		// These are the significant transition points worth checkpointing.
	default:
		return
	}

	state := h.StateFunc()
	if state == nil {
		fmt.Fprintf(os.Stderr, "checkpoint: state unavailable for event %d, skipping\n", event.Kind)
		return
	}

	gitSHA := ""
	if h.GitSHAFunc != nil {
		gitSHA = h.GitSHAFunc()
	}

	cp := FromCycleState(state, h.PhaseID, h.NebulaName, gitSHA)

	if err := Save(h.Dir, cp); err != nil {
		fmt.Fprintf(os.Stderr, "checkpoint: failed to save: %v\n", err)
	}
}
