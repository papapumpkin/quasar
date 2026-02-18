// Package cmd provides CLI commands for quasar.
//
// This file contains adapter types that bridge between the loop and nebula
// packages. They wrap loop.Loop (or its construction parameters) to satisfy
// the nebula.PhaseRunner interface, allowing the nebula orchestrator to drive
// the coder-reviewer loop without depending on loop internals directly.
package cmd

import (
	"context"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/beads"
	"github.com/papapumpkin/quasar/internal/loop"
	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/tui"
)

// loopAdapter wraps *loop.Loop to satisfy nebula.PhaseRunner.
type loopAdapter struct {
	loop *loop.Loop
}

func (a *loopAdapter) RunExistingPhase(ctx context.Context, phaseID, beadID, phaseDescription string, exec nebula.ResolvedExecution) (*nebula.PhaseRunnerResult, error) {
	// Apply per-phase execution overrides to the loop.
	if exec.MaxReviewCycles > 0 {
		a.loop.MaxCycles = exec.MaxReviewCycles
	}
	if exec.MaxBudgetUSD > 0 {
		a.loop.MaxBudgetUSD = exec.MaxBudgetUSD
	}
	if exec.Model != "" {
		a.loop.Model = exec.Model
	}

	result, err := a.loop.RunExistingTask(ctx, beadID, phaseDescription)
	if err != nil {
		return nil, err
	}
	return toPhaseRunnerResult(result), nil
}

func (a *loopAdapter) GenerateCheckpoint(ctx context.Context, beadID, phaseDescription string) (string, error) {
	return a.loop.GenerateCheckpoint(ctx, beadID, phaseDescription)
}

// tuiLoopAdapter creates a fresh loop per phase with a phase-specific PhaseUIBridge.
// This ensures each nebula phase sends UI messages tagged with its phase ID,
// enabling the TUI to track per-phase cycle timelines independently.
type tuiLoopAdapter struct {
	program      *tui.Program
	invoker      agent.Invoker
	beads        beads.Client
	git          loop.CycleCommitter
	maxCycles    int
	maxBudget    float64
	model        string
	coderPrompt  string
	reviewPrompt string
	workDir      string
}

func (a *tuiLoopAdapter) RunExistingPhase(ctx context.Context, phaseID, beadID, phaseDescription string, exec nebula.ResolvedExecution) (*nebula.PhaseRunnerResult, error) {
	// Create a per-phase UI bridge so messages carry the phase ID.
	phaseUI := tui.NewPhaseUIBridge(a.program, phaseID, a.workDir)

	l := &loop.Loop{
		Invoker:      a.invoker,
		UI:           phaseUI,
		Git:          a.git,
		Hooks:        []loop.Hook{&loop.BeadHook{Beads: a.beads, UI: phaseUI}},
		MaxCycles:    a.maxCycles,
		MaxBudgetUSD: a.maxBudget,
		Model:        a.model,
		CoderPrompt:  a.coderPrompt,
		ReviewPrompt: a.reviewPrompt,
		WorkDir:      a.workDir,
	}

	// Apply per-phase execution overrides.
	if exec.MaxReviewCycles > 0 {
		l.MaxCycles = exec.MaxReviewCycles
	}
	if exec.MaxBudgetUSD > 0 {
		l.MaxBudgetUSD = exec.MaxBudgetUSD
	}
	if exec.Model != "" {
		l.Model = exec.Model
	}

	result, err := l.RunExistingTask(ctx, beadID, phaseDescription)
	if err != nil {
		return nil, err
	}
	return toPhaseRunnerResult(result), nil
}

func (a *tuiLoopAdapter) GenerateCheckpoint(ctx context.Context, beadID, phaseDescription string) (string, error) {
	phaseUI := tui.NewPhaseUIBridge(a.program, "checkpoint", a.workDir)
	l := &loop.Loop{
		Invoker:      a.invoker,
		UI:           phaseUI,
		Git:          a.git,
		Hooks:        []loop.Hook{&loop.BeadHook{Beads: a.beads, UI: phaseUI}},
		MaxCycles:    a.maxCycles,
		MaxBudgetUSD: a.maxBudget,
		Model:        a.model,
		CoderPrompt:  a.coderPrompt,
		ReviewPrompt: a.reviewPrompt,
		WorkDir:      a.workDir,
	}
	return l.GenerateCheckpoint(ctx, beadID, phaseDescription)
}

// toPhaseRunnerResult converts a loop.TaskResult to nebula.PhaseRunnerResult.
func toPhaseRunnerResult(result *loop.TaskResult) *nebula.PhaseRunnerResult {
	return &nebula.PhaseRunnerResult{
		TotalCostUSD: result.TotalCostUSD,
		CyclesUsed:   result.CyclesUsed,
		Report:       result.Report,
	}
}
