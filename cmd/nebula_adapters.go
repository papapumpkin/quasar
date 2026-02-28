// Package cmd provides CLI commands for quasar.
//
// This file contains adapter types that bridge between the loop and nebula
// packages. They wrap loop.Loop (or its construction parameters) to satisfy
// the nebula.PhaseRunner interface, allowing the nebula orchestrator to drive
// the coder-reviewer loop without depending on loop internals directly.
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/beads"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/loop"
	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/tui"
)

// loopAdapter wraps *loop.Loop to satisfy nebula.PhaseRunner.
type loopAdapter struct {
	loop *loop.Loop
}

func (a *loopAdapter) RunExistingPhase(ctx context.Context, phaseID, beadID, phaseTitle, phaseDescription string, exec nebula.ResolvedExecution) (*nebula.PhaseRunnerResult, error) {
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
	a.loop.CommitSummary = phaseTitle

	result, err := a.loop.RunExistingTask(ctx, beadID, phaseDescription)
	if err != nil {
		if result != nil {
			return toPhaseRunnerResult(result), err
		}
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
	program          *tui.Program
	invoker          agent.Invoker
	beads            beads.Client
	git              loop.CycleCommitter
	linter           loop.Linter
	maxCycles        int
	maxBudget        float64
	model            string
	coderPrompt      string
	reviewPrompt     string
	workDir          string
	fabric           fabric.Fabric // nil when fabric is not configured
	projectContext   string        // Deterministic project snapshot for prompt caching.
	maxContextTokens int           // Token budget for context injection. 0 = use default.
}

func (a *tuiLoopAdapter) RunExistingPhase(ctx context.Context, phaseID, beadID, phaseTitle, phaseDescription string, exec nebula.ResolvedExecution) (*nebula.PhaseRunnerResult, error) {
	// Create a per-phase UI bridge so messages carry the phase ID.
	phaseUI := tui.NewPhaseUIBridge(a.program, phaseID, a.workDir)

	l := &loop.Loop{
		Invoker:          a.invoker,
		UI:               phaseUI,
		Git:              a.git,
		Hooks:            []loop.Hook{&loop.BeadHook{Beads: a.beads, UI: phaseUI}},
		Linter:           a.linter,
		MaxCycles:        a.maxCycles,
		MaxBudgetUSD:     a.maxBudget,
		Model:            a.model,
		CoderPrompt:      a.coderPrompt,
		ReviewPrompt:     a.reviewPrompt,
		WorkDir:          a.workDir,
		CommitSummary:    phaseTitle,
		Fabric:           a.fabric,
		FabricEnabled:    a.fabric != nil,
		ProjectContext:   a.projectContext,
		MaxContextTokens: a.maxContextTokens,
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

	// After the loop completes, emit fabric events if fabric is available.
	a.emitFabricEvents(ctx, phaseID, phaseUI)

	if err != nil {
		if result != nil {
			return toPhaseRunnerResult(result), err
		}
		return nil, err
	}
	return toPhaseRunnerResult(result), nil
}

// emitFabricEvents queries the fabric for entanglements and discoveries
// produced by this phase and emits the corresponding TUI messages.
func (a *tuiLoopAdapter) emitFabricEvents(ctx context.Context, phaseID string, phaseUI *tui.PhaseUIBridge) {
	if a.fabric == nil {
		return
	}
	// Emit entanglement update with the full list.
	if ents, err := a.fabric.AllEntanglements(ctx); err == nil && len(ents) > 0 {
		phaseUI.EntanglementPublished(ents)
	}
	// Emit discoveries posted by this phase.
	if discs, err := a.fabric.Discoveries(ctx, phaseID); err == nil {
		for _, d := range discs {
			phaseUI.DiscoveryPosted(d)
		}
	}
}

func (a *tuiLoopAdapter) GenerateCheckpoint(ctx context.Context, beadID, phaseDescription string) (string, error) {
	phaseUI := tui.NewPhaseUIBridge(a.program, "checkpoint", a.workDir)
	l := &loop.Loop{
		Invoker:          a.invoker,
		UI:               phaseUI,
		Git:              a.git,
		Hooks:            []loop.Hook{&loop.BeadHook{Beads: a.beads, UI: phaseUI}},
		Linter:           a.linter,
		MaxCycles:        a.maxCycles,
		MaxBudgetUSD:     a.maxBudget,
		Model:            a.model,
		CoderPrompt:      a.coderPrompt,
		ReviewPrompt:     a.reviewPrompt,
		WorkDir:          a.workDir,
		ProjectContext:   a.projectContext,
		MaxContextTokens: a.maxContextTokens,
	}
	return l.GenerateCheckpoint(ctx, beadID, phaseDescription)
}

// toPhaseRunnerResult converts a loop.TaskResult to nebula.PhaseRunnerResult.
func toPhaseRunnerResult(result *loop.TaskResult) *nebula.PhaseRunnerResult {
	return &nebula.PhaseRunnerResult{
		TotalCostUSD:   result.TotalCostUSD,
		CyclesUsed:     result.CyclesUsed,
		Report:         result.Report,
		BaseCommitSHA:  result.BaseCommitSHA,
		FinalCommitSHA: result.FinalCommitSHA,
	}
}

// fabricComponents holds initialized fabric infrastructure for passing to
// WorkerGroup options. When there are no inter-phase dependencies, all fields are nil.
type fabricComponents struct {
	Fabric    fabric.Fabric
	Poller    fabric.Poller
	Publisher *fabric.Publisher
	closeFn   func() error
}

// Close releases fabric resources. Safe to call when fc is nil or Fabric is nil.
func (fc *fabricComponents) Close() error {
	if fc == nil || fc.Fabric == nil {
		return nil
	}
	return fc.closeFn()
}

// WorkerGroupOptions returns the WithFabric/WithPoller/WithPublisher options.
// Returns nil when fabric is not active.
func (fc *fabricComponents) WorkerGroupOptions() []nebula.Option {
	if fc == nil || fc.Fabric == nil {
		return nil
	}
	return []nebula.Option{
		nebula.WithFabric(fc.Fabric),
		nebula.WithPoller(fc.Poller),
		nebula.WithPublisher(fc.Publisher),
	}
}

// initFabric creates the fabric infrastructure when the DAG has inter-phase
// dependencies. When no phases have dependencies, it returns a zero-value
// fabricComponents (all nil fields). The caller must defer fc.Close().
func initFabric(ctx context.Context, n *nebula.Nebula, dir, workDir string, inv agent.Invoker) (*fabricComponents, error) {
	if !n.HasDependencies() {
		return &fabricComponents{}, nil
	}

	fabricDir := filepath.Join(workDir, ".quasar")
	if err := os.MkdirAll(fabricDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating fabric directory: %w", err)
	}

	// Ensure the telemetry directory and file exist so that TelemetryBridge
	// can start tailing immediately when the scratchpad is opened.
	telemetryDir := filepath.Join(workDir, ".quasar", "telemetry")
	if err := os.MkdirAll(telemetryDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating telemetry directory: %w", err)
	}
	telemetryFile := filepath.Join(telemetryDir, "current.jsonl")
	if _, err := os.Stat(telemetryFile); os.IsNotExist(err) {
		if f, err := os.Create(telemetryFile); err == nil {
			f.Close()
		}
	}

	fabricPath := filepath.Join(fabricDir, "fabric.db")

	fab, err := fabric.NewSQLiteFabric(ctx, fabricPath)
	if err != nil {
		return nil, fmt.Errorf("creating fabric: %w", err)
	}

	// Build phase inputs for the static scanner.
	phaseInputs := make([]fabric.PhaseInput, len(n.Phases))
	for i, p := range n.Phases {
		phaseInputs[i] = fabric.PhaseInput{
			ID:        p.ID,
			Body:      p.Body,
			Scope:     p.Scope,
			DependsOn: p.DependsOn,
		}
	}

	// Try the deterministic contract poller first. Fall back to LLM poller
	// if the static scanner fails (e.g. scope globs don't resolve, parse errors).
	var poller fabric.Poller
	scanner := &fabric.StaticScanner{WorkDir: workDir}
	contracts, scanErr := scanner.Scan(phaseInputs)
	if scanErr == nil && len(contracts) > 0 {
		contractMap := make(map[string]*fabric.PhaseContract, len(contracts))
		for i := range contracts {
			contractMap[contracts[i].PhaseID] = &contracts[i]
		}
		poller = &fabric.ContractPoller{
			Contracts: contractMap,
			MatchMode: fabric.MatchName,
		}
		fmt.Fprintf(os.Stderr, "Dispatch: deterministic contract poller (%d contracts loaded)\n", len(contracts))
	} else {
		// Fall back to LLM poller.
		if scanErr != nil {
			fmt.Fprintf(os.Stderr, "warning: static scan failed, falling back to LLM poller: %v\n", scanErr)
		} else {
			fmt.Fprintf(os.Stderr, "warning: static scan produced no contracts, falling back to LLM poller\n")
		}
		phaseMap := make(map[string]*fabric.PhaseSpec, len(n.Phases))
		for i := range n.Phases {
			phaseMap[n.Phases[i].ID] = &fabric.PhaseSpec{
				ID:   n.Phases[i].ID,
				Body: n.Phases[i].Body,
			}
		}
		poller = &fabric.LLMPoller{
			Invoker: inv,
			Phases:  phaseMap,
		}
		fmt.Fprintf(os.Stderr, "Dispatch: LLM poller (fallback)\n")
	}

	pub := &fabric.Publisher{
		Fabric:  fab,
		WorkDir: workDir,
		Logger:  os.Stderr,
	}

	// Seed all phases as queued so the fabric has entries from the start.
	for _, p := range n.Phases {
		if err := fab.SetPhaseState(ctx, p.ID, fabric.StateQueued); err != nil {
			fab.Close()
			return nil, fmt.Errorf("seeding phase state for %s: %w", p.ID, err)
		}
	}

	return &fabricComponents{
		Fabric:    fab,
		Poller:    poller,
		Publisher: pub,
		closeFn:   fab.Close,
	}, nil
}
