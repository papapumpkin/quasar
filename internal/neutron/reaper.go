package neutron

import (
	"context"
	"fmt"
	"time"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// Default thresholds for stale state detection.
const (
	DefaultStaleClaim = 30 * time.Minute
	DefaultStaleEpoch = 1 * time.Hour
)

// ReapAction describes a cleanup action taken by the Reaper.
type ReapAction struct {
	Kind    string // "released_claim", "flagged_epoch"
	Details string
}

// Reaper identifies and cleans up stale state in a fabric.
type Reaper struct {
	Fabric     fabric.Fabric
	StaleClaim time.Duration    // claims older than this with no running task get released
	StaleEpoch time.Duration    // epochs with no state transitions in this duration get flagged
	Now        func() time.Time // injectable clock for testing; defaults to time.Now
}

// Run checks for stale claims and epochs, releasing or flagging as appropriate.
//
// Claims older than StaleClaim whose owning task is not in "running" state are
// released automatically. Epochs (identified by any task having no recent state
// change) with no transitions in StaleEpoch are flagged for human review — they
// are NOT auto-purged.
func (r *Reaper) Run(ctx context.Context) ([]ReapAction, error) {
	staleClaim := r.StaleClaim
	if staleClaim == 0 {
		staleClaim = DefaultStaleClaim
	}
	staleEpoch := r.StaleEpoch
	if staleEpoch == 0 {
		staleEpoch = DefaultStaleEpoch
	}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}

	var actions []ReapAction

	// Reap stale claims.
	claimActions, err := r.reapClaims(ctx, now, staleClaim)
	if err != nil {
		return nil, fmt.Errorf("neutron: reap claims: %w", err)
	}
	actions = append(actions, claimActions...)

	// Flag stale epochs.
	epochActions, err := r.flagStaleEpochs(ctx, now, staleEpoch)
	if err != nil {
		return nil, fmt.Errorf("neutron: flag stale epochs: %w", err)
	}
	actions = append(actions, epochActions...)

	return actions, nil
}

// reapClaims releases file claims that are older than the threshold and whose
// owning task is not currently running. Since ReleaseClaims removes all claims
// for an owner at once, we track already-released owners to avoid redundant
// DB calls and produce accurate action reports.
func (r *Reaper) reapClaims(ctx context.Context, now time.Time, threshold time.Duration) ([]ReapAction, error) {
	claims, err := r.Fabric.AllClaims(ctx)
	if err != nil {
		return nil, fmt.Errorf("read claims: %w", err)
	}

	states, err := r.Fabric.AllPhaseStates(ctx)
	if err != nil {
		return nil, fmt.Errorf("read phase states: %w", err)
	}

	released := make(map[string]bool)
	var actions []ReapAction
	for _, c := range claims {
		// Skip if we already released all claims for this owner.
		if released[c.OwnerTask] {
			continue
		}

		age := now.Sub(c.ClaimedAt)
		if age < threshold {
			continue
		}

		// Only release if the owning task is not actively running.
		state := states[c.OwnerTask]
		if state == fabric.StateRunning {
			continue
		}

		if err := r.Fabric.ReleaseClaims(ctx, c.OwnerTask); err != nil {
			return actions, fmt.Errorf("release claims for %s: %w", c.OwnerTask, err)
		}
		released[c.OwnerTask] = true

		actions = append(actions, ReapAction{
			Kind:    "released_claim",
			Details: fmt.Sprintf("released claims held by %q (age: %s, state: %s)", c.OwnerTask, age.Round(time.Second), state),
		})
	}
	return actions, nil
}

// flagStaleEpochs identifies epochs where all tasks are terminal and leftover
// state (claims or unresolved discoveries) has persisted beyond the StaleEpoch
// threshold. Since the Fabric interface does not expose updated_at timestamps
// directly, claim age is used as a proxy: if any claim is older than the
// threshold, the epoch is considered stale.
func (r *Reaper) flagStaleEpochs(ctx context.Context, now time.Time, threshold time.Duration) ([]ReapAction, error) {
	states, err := r.Fabric.AllPhaseStates(ctx)
	if err != nil {
		return nil, fmt.Errorf("read phase states: %w", err)
	}

	if len(states) == 0 {
		return nil, nil
	}

	// Only flag when every task is in a terminal state.
	allTerminal := true
	for _, state := range states {
		if state != fabric.StateDone && state != fabric.StateFailed {
			allTerminal = false
			break
		}
	}

	if !allTerminal {
		return nil, nil
	}

	// Check for leftover claims — indicates stale state.
	claims, err := r.Fabric.AllClaims(ctx)
	if err != nil {
		return nil, fmt.Errorf("read claims for epoch check: %w", err)
	}

	// Check for unresolved discoveries.
	unresolved, err := r.Fabric.UnresolvedDiscoveries(ctx)
	if err != nil {
		return nil, fmt.Errorf("read unresolved discoveries: %w", err)
	}

	if len(claims) == 0 && len(unresolved) == 0 {
		return nil, nil
	}

	// Use claim age as a proxy for epoch staleness. If no claims exist,
	// fall back to flagging unconditionally (unresolved discoveries alone
	// with all-terminal tasks is inherently stale).
	if len(claims) > 0 {
		stale := false
		for _, c := range claims {
			if now.Sub(c.ClaimedAt) >= threshold {
				stale = true
				break
			}
		}
		if !stale {
			return nil, nil
		}
	}

	var actions []ReapAction
	actions = append(actions, ReapAction{
		Kind: "flagged_epoch",
		Details: fmt.Sprintf("all %d tasks terminal but %d claims and %d unresolved discoveries remain (stale > %s)",
			len(states), len(claims), len(unresolved), threshold),
	})
	return actions, nil
}
