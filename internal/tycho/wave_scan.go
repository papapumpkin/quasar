package tycho

import (
	"context"
	"fmt"
	"io"

	"github.com/papapumpkin/quasar/internal/dag"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// WaveScanner walks execution waves in topological order, polling each
// phase via the configured Poller. When a phase in wave N cannot proceed,
// its entire downstream subtree (transitive dependents) is pruned from
// consideration in subsequent waves, avoiding unnecessary poll calls.
type WaveScanner struct {
	Poller   fabric.Poller
	Blocked  *fabric.BlockedTracker
	Pushback *fabric.PushbackHandler
	Fabric   fabric.Fabric
	DAG      *dag.DAG
	Logger   io.Writer
}

// ScanWaves evaluates phases wave-by-wave. Returns the set of phases that
// can proceed and a map of pruned phase IDs to their prune reason.
//
// Phases not in the eligible set are skipped (already done, failed, or
// in-flight). Phases that poll non-PROCEED are blocked and their
// transitive descendants are pruned. Pruned phases are never polled.
func (ws *WaveScanner) ScanWaves(
	ctx context.Context,
	waves []dag.Wave,
	eligible map[string]bool,
	snap fabric.FabricSnapshot,
) (proceed []string, pruned map[string]string) {
	pruned = make(map[string]string)

	for _, wave := range waves {
		for _, phaseID := range wave.NodeIDs {
			if !eligible[phaseID] {
				continue // already done, failed, or in-flight
			}
			if reason, ok := pruned[phaseID]; ok {
				fmt.Fprintf(ws.logger(), "  Phase %q pruned: %s\n", phaseID, reason)
				continue // ancestor couldn't proceed
			}

			// Skip already-blocked phases — they await re-evaluation.
			if ws.Blocked != nil && ws.Blocked.Get(phaseID) != nil {
				continue
			}

			// Phases previously overridden by the pushback handler skip
			// polling entirely — they proceed without re-interrogation.
			if ws.Blocked != nil && ws.Blocked.IsOverridden(phaseID) {
				proceed = append(proceed, phaseID)
				continue
			}

			result, err := ws.Poller.Poll(ctx, phaseID, snap)
			if err != nil {
				fmt.Fprintf(ws.logger(), "warning: poll failed for phase %q: %v\n", phaseID, err)
				proceed = append(proceed, phaseID) // on error, proceed optimistically
				continue
			}

			if result.Decision == fabric.PollProceed {
				if ws.Fabric != nil {
					if setErr := ws.Fabric.SetPhaseState(ctx, phaseID, fabric.StateRunning); setErr != nil {
						fmt.Fprintf(ws.logger(), "warning: failed to set fabric state for %q: %v\n", phaseID, setErr)
					}
				}
				proceed = append(proceed, phaseID)
			} else {
				ws.handleBlock(ctx, phaseID, result, snap)
				ws.pruneDescendants(phaseID, result.Reason, pruned)
			}
		}
	}

	return proceed, pruned
}

// handleBlock records a blocked phase and runs the pushback handler.
func (ws *WaveScanner) handleBlock(ctx context.Context, phaseID string, result fabric.PollResult, snap fabric.FabricSnapshot) {
	if ws.Blocked == nil {
		return
	}
	ws.Blocked.Block(phaseID, result)
	bp := ws.Blocked.Get(phaseID)

	if ws.Fabric != nil {
		if setErr := ws.Fabric.SetPhaseState(ctx, phaseID, fabric.StateBlocked); setErr != nil {
			fmt.Fprintf(ws.logger(), "warning: failed to set fabric state for %q: %v\n", phaseID, setErr)
		}
	}

	if ws.Pushback != nil {
		action := ws.Pushback.Handle(ctx, bp, snap.InProgress, snap)
		switch action {
		case fabric.ActionRetry:
			fmt.Fprintf(ws.logger(), "  Phase %q blocked: %s (retry %d)\n",
				phaseID, result.Reason, bp.RetryCount)
		case fabric.ActionProceed:
			ws.Blocked.Unblock(phaseID)
			ws.Blocked.Override(phaseID)
		}
	}
}

// pruneDescendants marks all transitive dependents of phaseID as pruned.
func (ws *WaveScanner) pruneDescendants(phaseID, reason string, pruned map[string]string) {
	if ws.DAG == nil {
		return
	}
	for _, desc := range ws.DAG.Descendants(phaseID) {
		if _, already := pruned[desc]; !already {
			pruned[desc] = fmt.Sprintf("upstream %s blocked: %s", phaseID, reason)
		}
	}
}

// logger returns the effective log writer (io.Discard if Logger is nil).
func (ws *WaveScanner) logger() io.Writer {
	if ws.Logger != nil {
		return ws.Logger
	}
	return io.Discard
}
