package nebula

import (
	"context"
	"fmt"
	"strings"

	"github.com/papapumpkin/quasar/internal/beads"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// attemptHealing runs the healing pipeline for a failed phase: analyzes the
// failure, invokes the architect for a remediation spec, inserts the
// remediation phase into the live DAG, and signals it for execution.
//
// Must NOT be called with wg.mu held. Safe to call from a goroutine.
func (wg *WorkerGroup) attemptHealing(ctx context.Context, phaseID string, err error, result *PhaseRunnerResult) {
	// Analyze the failure.
	var fctx *FailureContext
	if result != nil {
		fctx = &FailureContext{
			Cycle:        result.CyclesUsed,
			TotalCostUSD: result.TotalCostUSD,
			AllFindings:  result.AllFindings,
		}
	}
	diag := AnalyzeFailure(phaseID, err, result, fctx)

	// Check healing eligibility under lock.
	wg.mu.Lock()
	attempts := wg.healAttempts[phaseID]
	canHeal := wg.healingPolicy.CanHeal(diag, attempts)
	if !canHeal {
		wg.mu.Unlock()
		fmt.Fprintf(wg.logger(), "healing: phase %q not healable (kind=%s, attempts=%d)\n", phaseID, diag.Kind, attempts)
		return
	}
	wg.healAttempts[phaseID]++
	wg.mu.Unlock()

	fmt.Fprintf(wg.logger(), "healing: attempting remediation for phase %q (kind=%s)\n", phaseID, diag.Kind)

	// Snapshot nebula and locate the failed spec.
	wg.mu.Lock()
	phasesByID := wg.tracker.PhasesByIDMap()
	failedSpec := phasesByID[phaseID]
	nebSnap := wg.Nebula.Snapshot()
	wg.mu.Unlock()

	if failedSpec == nil {
		fmt.Fprintf(wg.logger(), "healing: phase %q not found in tracker, aborting\n", phaseID)
		return
	}

	// Build and run the architect request.
	if wg.Invoker == nil {
		fmt.Fprintf(wg.logger(), "healing: no invoker configured, cannot generate remediation for %q\n", phaseID)
		return
	}

	req := BuildRemediationRequest(diag, nebSnap, failedSpec)
	archResult, archErr := RunArchitect(ctx, wg.Invoker, req)
	if archErr != nil {
		fmt.Fprintf(wg.logger(), "healing: architect failed for %q: %v\n", phaseID, archErr)
		return
	}

	// Finalize the remediation spec.
	archResult = FinalizeRemediationSpec(archResult, diag, failedSpec)
	remSpec := &archResult.PhaseSpec

	// Insert into live DAG under lock.
	wg.mu.Lock()
	var liveGraph = wg.hotReload.liveGraph
	var livePhasesMap = wg.hotReload.livePhasesByID
	if liveGraph == nil {
		wg.mu.Unlock()
		fmt.Fprintf(wg.logger(), "healing: live DAG not initialized, aborting remediation for %q\n", phaseID)
		return
	}

	rewired, insertErr := InsertRemediationPhase(liveGraph, phaseID, remSpec)
	if insertErr != nil {
		wg.mu.Unlock()
		fmt.Fprintf(wg.logger(), "healing: DAG insertion failed for %q: %v\n", phaseID, insertErr)
		return
	}

	// Register the remediation phase in all live data structures.
	wg.Nebula.Phases = append(wg.Nebula.Phases, *remSpec)
	livePhasesMap[remSpec.ID] = &wg.Nebula.Phases[len(wg.Nebula.Phases)-1]

	// Update DependsOn on rewired phases' specs to reflect the new edges.
	for _, depID := range rewired {
		if depSpec, ok := livePhasesMap[depID]; ok {
			// Remove failedID from DependsOn and add remediation ID.
			newDeps := make([]string, 0, len(depSpec.DependsOn))
			for _, d := range depSpec.DependsOn {
				if d != phaseID {
					newDeps = append(newDeps, d)
				}
			}
			newDeps = append(newDeps, remSpec.ID)
			depSpec.DependsOn = newDeps
		}
	}
	wg.mu.Unlock()

	// Create a bead for the remediation phase (outside lock to avoid blocking).
	beadID := ""
	if wg.BeadsClient != nil {
		var createErr error
		beadID, createErr = wg.BeadsClient.Create(ctx, remSpec.Title, beads.CreateOpts{
			Description: remSpec.Body,
			Type:        remSpec.Type,
			Labels:      remSpec.Labels,
			Assignee:    remSpec.Assignee,
			Priority:    priorityStr(remSpec.Priority),
		})
		if createErr != nil {
			fmt.Fprintf(wg.logger(), "healing: failed to create bead for remediation phase %q: %v\n", remSpec.ID, createErr)
			// Continue without bead — phase will fail at executePhase, which is
			// acceptable as a degraded-mode fallback.
		}
	}

	// Register state under lock.
	wg.mu.Lock()
	wg.State.SetPhaseState(remSpec.ID, beadID, PhaseStatusPending)
	wg.progress.SaveState()
	wg.progress.ReportProgress()

	// Signal hot-added readiness.
	if wg.hotReload != nil {
		wg.hotReload.CheckHotAddedReady()
	}

	// Set fabric state for remediation phase.
	if wg.Fabric != nil {
		if stateErr := wg.Fabric.SetPhaseState(ctx, remSpec.ID, fabric.StateQueued); stateErr != nil {
			fmt.Fprintf(wg.logger(), "healing: failed to set fabric state for %q: %v\n", remSpec.ID, stateErr)
		}
	}
	wg.mu.Unlock()

	// Notify TUI (callbacks must not hold the lock).
	if wg.OnHotAdd != nil {
		wg.OnHotAdd(remSpec.ID, remSpec.Title, remSpec.DependsOn)
	}

	// Post a hail for observability.
	if wg.OnHail != nil {
		wg.OnHail(phaseID, fabric.Discovery{
			Kind:   "healing",
			Detail: fmt.Sprintf("Phase %q failed (%s); remediation phase %q inserted, rewiring dependents: %s", phaseID, diag.Kind, remSpec.ID, strings.Join(rewired, ", ")),
		})
	}

	fmt.Fprintf(wg.logger(), "healing: remediation phase %q inserted for failed %q (rewired: %s)\n", remSpec.ID, phaseID, strings.Join(rewired, ", "))
}
