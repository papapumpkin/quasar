package nebula

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/papapumpkin/quasar/internal/beads"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/telemetry"
)

// emitHealing is a nil-safe helper that emits a healing telemetry event.
// It is a no-op when wg.Metrics or wg.Metrics.Telemetry is nil.
func (wg *WorkerGroup) emitHealing(kind string, taskID string, data any) {
	if wg.Metrics == nil || wg.Metrics.Telemetry == nil {
		return
	}
	if err := wg.Metrics.Telemetry.Emit(telemetry.Event{
		Timestamp: time.Now(),
		Kind:      kind,
		TaskID:    taskID,
		Data:      data,
	}); err != nil {
		fmt.Fprintf(wg.logger(), "healing: telemetry emit %s: %v\n", kind, err)
	}
}

// healingSkipReason returns a human-readable reason string for why healing
// was skipped, suitable for telemetry and logging.
func healingSkipReason(policy HealingPolicy, diag *FailureDiagnosis, attempts int) string {
	if !policy.Enabled {
		return "policy_disabled"
	}
	if diag == nil || !diag.Healable {
		return "unhealable"
	}
	if attempts >= policy.MaxAttempts {
		return "max_attempts"
	}
	if policy.BudgetReserve <= 0 {
		return "no_budget"
	}
	return "unknown"
}

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

	// Build partial work snapshot from the failed phase's result.
	if result != nil && result.BaseCommitSHA != "" {
		var diffLister GitDiffLister
		if wg.Committer != nil {
			diffLister = &gitCommitterDiffLister{committer: wg.Committer}
		}
		pw, pwErr := BuildPartialWork(ctx, result, diag.Findings, diffLister)
		if pwErr != nil {
			fmt.Fprintf(wg.logger(), "healing: failed to build partial work for %q: %v\n", phaseID, pwErr)
			// pw may still be partially populated; attach it anyway.
		}
		diag.PartialWork = pw
	}

	// Emit healing.start telemetry.
	wg.emitHealing(telemetry.KindHealingStart, phaseID, map[string]any{
		"phase_id":     phaseID,
		"failure_kind": string(diag.Kind),
		"cycles_used":  diag.CyclesUsed,
		"budget_spent": diag.BudgetSpent,
	})

	// Check healing eligibility under lock.
	wg.mu.Lock()
	attempts := wg.healAttempts[phaseID]
	canHeal := wg.healingPolicy.CanHeal(diag, attempts)
	if !canHeal {
		skipReason := healingSkipReason(wg.healingPolicy, diag, attempts)
		wg.mu.Unlock()

		// Emit healing.skipped telemetry.
		wg.emitHealing(telemetry.KindHealingSkipped, phaseID, map[string]any{
			"phase_id": phaseID,
			"reason":   skipReason,
		})
		fmt.Fprintf(wg.logger(), "healing: phase %q not healable (kind=%s, attempts=%d, reason=%s)\n", phaseID, diag.Kind, attempts, skipReason)
		return
	}
	wg.healAttempts[phaseID]++
	wg.mu.Unlock()

	fmt.Fprintf(wg.logger(), "healing: attempting remediation for phase %q (kind=%s)\n", phaseID, diag.Kind)

	// Set fabric state to "healing" before architect invocation.
	if wg.Fabric != nil {
		if stateErr := wg.Fabric.SetPhaseState(ctx, phaseID, fabric.StateHealing); stateErr != nil {
			fmt.Fprintf(wg.logger(), "healing: failed to set fabric healing state for %q: %v\n", phaseID, stateErr)
		}
	}

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

	// Inject healing context into the remediation phase body so the coder
	// knows about partial work and is instructed to preserve it.
	if hctx := HealingContext(diag.PartialWork); hctx != "" {
		remSpec.Body = hctx + "\n" + remSpec.Body
	}

	// Emit healing.plan telemetry.
	wg.emitHealing(telemetry.KindHealingPlan, phaseID, map[string]any{
		"phase_id":       phaseID,
		"remediation_id": remSpec.ID,
		"title":          remSpec.Title,
	})

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

	// Emit healing.insert telemetry.
	wg.emitHealing(telemetry.KindHealingInsert, phaseID, map[string]any{
		"phase_id":           phaseID,
		"remediation_id":     remSpec.ID,
		"rewired_dependents": rewired,
	})

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

	// Send TUI healing attempt message.
	if wg.OnHail != nil {
		wg.OnHail(phaseID, fabric.Discovery{
			Kind:   "healing",
			Detail: fmt.Sprintf("Auto-healing activated for %s (%s). Remediation phase: %s — %s", phaseID, diag.Kind, remSpec.ID, remSpec.Title),
		})
	}

	fmt.Fprintf(wg.logger(), "healing: remediation phase %q inserted for failed %q (rewired: %s)\n", remSpec.ID, phaseID, strings.Join(rewired, ", "))
}

// gitCommitterDiffLister adapts a GitCommitter to the GitDiffLister interface
// by parsing the full diff output for file names.
type gitCommitterDiffLister struct {
	committer GitCommitter
}

// DiffFileList returns the list of files changed between base and head by
// parsing the output of GitCommitter.DiffRange for diff headers.
func (g *gitCommitterDiffLister) DiffFileList(ctx context.Context, base, head string) ([]string, error) {
	diff, err := g.committer.DiffRange(ctx, base, head)
	if err != nil {
		return nil, fmt.Errorf("diffing %s..%s: %w", base, head, err)
	}
	return parseDiffFileNames(diff), nil
}

// parseDiffFileNames extracts unique file paths from unified diff output
// by scanning for "diff --git a/... b/..." headers.
func parseDiffFileNames(diff string) []string {
	const prefix = "diff --git a/"
	seen := make(map[string]bool)
	var files []string
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		// Format: "diff --git a/<path> b/<path>"
		rest := line[len(prefix):]
		spaceIdx := strings.Index(rest, " b/")
		if spaceIdx < 0 {
			continue
		}
		name := rest[:spaceIdx]
		if !seen[name] {
			seen[name] = true
			files = append(files, name)
		}
	}
	return files
}
