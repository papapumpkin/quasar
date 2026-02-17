package nebula

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/papapumpkin/quasar/internal/beads"
)

// CheckDependencies verifies that all external dependencies declared in the nebula are met.
// For requires_beads: each bead must exist and be closed.
// For requires_nebulae: each named nebula's state file must show all phases done.
func CheckDependencies(ctx context.Context, deps Dependencies, nebulaDir string, client beads.Client) error {
	var unmet []string

	for _, beadID := range deps.RequiresBeads {
		b, err := client.Show(ctx, beadID)
		if err != nil {
			unmet = append(unmet, fmt.Sprintf("bead %q: %v", beadID, err))
			continue
		}
		if b.Status != "closed" {
			unmet = append(unmet, fmt.Sprintf("bead %q: status is %q, expected closed", beadID, b.Status))
		}
	}

	for _, name := range deps.RequiresNebulae {
		stateDir := filepath.Join(filepath.Dir(nebulaDir), name)
		st, err := LoadState(stateDir)
		if err != nil {
			unmet = append(unmet, fmt.Sprintf("nebula %q: cannot load state: %v", name, err))
			continue
		}
		if len(st.Phases) == 0 {
			unmet = append(unmet, fmt.Sprintf("nebula %q: no phases found in state", name))
			continue
		}
		for phaseID, ps := range st.Phases {
			if ps.Status != PhaseStatusDone {
				unmet = append(unmet, fmt.Sprintf("nebula %q: phase %q is %s, not done", name, phaseID, ps.Status))
			}
		}
	}

	if len(unmet) > 0 {
		return fmt.Errorf("%w: %s", ErrUnmetDependency, strings.Join(unmet, "; "))
	}
	return nil
}

// BuildPlan diffs the desired nebula state against actual beads state,
// producing a plan of create/update/skip/close/retry actions.
func BuildPlan(ctx context.Context, n *Nebula, state *State, client beads.Client) (*Plan, error) {
	// Check external dependencies before building the plan.
	deps := n.Manifest.Dependencies
	if len(deps.RequiresBeads) > 0 || len(deps.RequiresNebulae) > 0 {
		if err := CheckDependencies(ctx, deps, n.Dir, client); err != nil {
			return nil, err
		}
	}

	plan := &Plan{
		NebulaName: n.Manifest.Nebula.Name,
	}

	// Build set of desired phase IDs.
	desired := make(map[string]bool)
	for _, p := range n.Phases {
		desired[p.ID] = true
	}

	// Determine action for each phase in the nebula.
	for _, p := range n.Phases {
		ps, exists := state.Phases[p.ID]

		if !exists {
			plan.Actions = append(plan.Actions, Action{
				PhaseID: p.ID,
				Type:    ActionCreate,
				Reason:  fmt.Sprintf("create bead for %q", p.Title),
			})
			continue
		}

		// Locked phases (in_progress) are skipped.
		if ps.Status == PhaseStatusInProgress {
			plan.Actions = append(plan.Actions, Action{
				PhaseID: p.ID,
				Type:    ActionSkip,
				Reason:  "locked (in_progress)",
			})
			continue
		}

		// Already done phases are skipped.
		if ps.Status == PhaseStatusDone {
			plan.Actions = append(plan.Actions, Action{
				PhaseID: p.ID,
				Type:    ActionSkip,
				Reason:  "already completed",
			})
			continue
		}

		// Failed phases are retried with a new bead.
		if ps.Status == PhaseStatusFailed {
			plan.Actions = append(plan.Actions, Action{
				PhaseID: p.ID,
				Type:    ActionRetry,
				Reason:  fmt.Sprintf("retrying failed phase (previous bead: %s)", ps.BeadID),
			})
			continue
		}

		// Phase exists in state but bead may need updating.
		if ps.BeadID != "" {
			// Verify bead still exists.
			_, err := client.Show(ctx, ps.BeadID)
			if err != nil {
				// Bead missing — recreate.
				plan.Actions = append(plan.Actions, Action{
					PhaseID: p.ID,
					Type:    ActionCreate,
					Reason:  fmt.Sprintf("bead %s not found, recreating", ps.BeadID),
				})
				continue
			}

			plan.Actions = append(plan.Actions, Action{
				PhaseID: p.ID,
				Type:    ActionUpdate,
				Reason:  fmt.Sprintf("update bead %s", ps.BeadID),
			})
			continue
		}

		// State entry without bead ID — create.
		plan.Actions = append(plan.Actions, Action{
			PhaseID: p.ID,
			Type:    ActionCreate,
			Reason:  fmt.Sprintf("create bead for %q (no bead ID in state)", p.Title),
		})
	}

	// Phases in state that are no longer in the nebula → close.
	for phaseID, ps := range state.Phases {
		if desired[phaseID] {
			continue
		}
		if ps.Status == PhaseStatusDone {
			continue
		}
		if ps.BeadID != "" {
			plan.Actions = append(plan.Actions, Action{
				PhaseID: phaseID,
				Type:    ActionClose,
				Reason:  "phase removed from nebula",
			})
		}
	}

	return plan, nil
}

// HasChanges returns true if the plan contains any non-skip actions.
func (p *Plan) HasChanges() bool {
	for _, a := range p.Actions {
		if a.Type != ActionSkip {
			return true
		}
	}
	return false
}

// RenderPlan writes a formatted execution plan summary to the given writer.
// It shows phases grouped into dependency waves and key statistics.
// Output uses ANSI colors consistent with checkpoint rendering.
func RenderPlan(w io.Writer, nebulaName string, waves []Wave, phaseCount int, budgetUSD float64, gate GateMode) {
	separator := ansi.Dim + "───────────────────────────────────────────────────" + ansi.Reset

	fmt.Fprintf(w, "\n"+ansi.Bold+ansi.Magenta+"── Nebula: %s (%s mode) ──"+ansi.Reset+"\n", nebulaName, gate)

	for _, wave := range waves {
		label := fmt.Sprintf("Wave %d", wave.Number)
		phases := strings.Join(wave.PhaseIDs, ", ")
		if len(wave.PhaseIDs) > 1 {
			fmt.Fprintf(w, "   "+ansi.Dim+"%s (parallel):"+ansi.Reset+" %s\n", label, phases)
		} else {
			fmt.Fprintf(w, "   "+ansi.Dim+"%s:"+ansi.Reset+"            %s\n", label, phases)
		}
	}

	fmt.Fprintln(w)
	var stats []string
	stats = append(stats, fmt.Sprintf("Phases: %d", phaseCount))
	if budgetUSD > 0 {
		stats = append(stats, fmt.Sprintf("Budget: $%.2f", budgetUSD))
	}
	stats = append(stats, fmt.Sprintf("Gate: %s", gate))
	fmt.Fprintf(w, "   %s\n", strings.Join(stats, " | "))

	fmt.Fprintln(w, separator)
}
