package nebula

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// GateAction represents the human's decision at a phase boundary.
type GateAction string

const (
	// GateActionAccept continues to the next phase.
	GateActionAccept GateAction = "accept"
	// GateActionReject marks the phase as failed and stops execution.
	GateActionReject GateAction = "reject"
	// GateActionRetry re-runs the current phase.
	GateActionRetry GateAction = "retry"
	// GateActionSkip stops the nebula gracefully, skipping remaining phases.
	GateActionSkip GateAction = "skip"
)

// Gater decides how to handle phase boundaries and plan approval.
// Implementations encapsulate the gate mode strategy (trust, watch, review, approve).
type Gater interface {
	// PhaseGate is called after a phase completes successfully.
	// Returns the action to take (accept, reject, retry, skip).
	PhaseGate(ctx context.Context, phase *PhaseSpec, cp *Checkpoint) (GateAction, error)
	// PlanGate is called before execution begins to optionally gate the plan.
	// Returns nil to proceed, ErrPlanRejected to stop.
	PlanGate(ctx context.Context, cp *Checkpoint) error
}

// GatePrompter handles human interaction at phase boundaries.
// Implementations display information and collect decisions from the user.
type GatePrompter interface {
	// Prompt displays the checkpoint and waits for a human decision.
	// Returns the chosen action.
	Prompt(ctx context.Context, cp *Checkpoint) (GateAction, error)
}

// GaterDeps carries dependencies needed by gate strategies that interact
// with the output system (e.g. watch mode checkpoint rendering).
type GaterDeps struct {
	Logger    io.Writer
	OutputMu  *sync.Mutex // serializes checkpoint output in watch mode
	Mu        *sync.Mutex // protects dashboard resume from concurrent state mutations
	Dashboard *Dashboard
}

// trustGater always proceeds without any user interaction.
type trustGater struct{}

// PhaseGate always accepts.
func (trustGater) PhaseGate(_ context.Context, _ *PhaseSpec, _ *Checkpoint) (GateAction, error) {
	return GateActionAccept, nil
}

// PlanGate always proceeds.
func (trustGater) PlanGate(_ context.Context, _ *Checkpoint) error {
	return nil
}

// watchGater renders checkpoint output but never blocks execution.
type watchGater struct {
	deps GaterDeps
}

// PhaseGate renders the checkpoint (with output serialization) and accepts.
func (g *watchGater) PhaseGate(_ context.Context, _ *PhaseSpec, cp *Checkpoint) (GateAction, error) {
	if cp != nil {
		g.deps.OutputMu.Lock()
		if g.deps.Dashboard != nil {
			g.deps.Dashboard.Pause()
		}
		RenderCheckpoint(g.deps.Logger, cp)
		if g.deps.Dashboard != nil {
			g.deps.Mu.Lock()
			g.deps.Dashboard.Resume()
			g.deps.Mu.Unlock()
		}
		g.deps.OutputMu.Unlock()
	}
	return GateActionAccept, nil
}

// PlanGate always proceeds (watch mode does not gate plans).
func (g *watchGater) PlanGate(_ context.Context, _ *Checkpoint) error {
	return nil
}

// reviewGater renders checkpoints and prompts the user for each phase,
// but does not gate the execution plan.
type reviewGater struct {
	prompter GatePrompter
	logger   io.Writer
}

// PhaseGate renders the checkpoint and prompts for a decision.
func (g *reviewGater) PhaseGate(ctx context.Context, _ *PhaseSpec, cp *Checkpoint) (GateAction, error) {
	if cp != nil {
		RenderCheckpoint(g.logger, cp)
	}
	action, err := g.prompter.Prompt(ctx, cp)
	if err != nil {
		fmt.Fprintf(g.logger, "warning: gate prompt failed: %v (defaulting to accept)\n", err)
		return GateActionAccept, nil
	}
	return action, nil
}

// PlanGate always proceeds (review mode does not gate plans).
func (g *reviewGater) PlanGate(_ context.Context, _ *Checkpoint) error {
	return nil
}

// approveGater renders checkpoints and prompts the user for each phase
// and also gates the execution plan for approval.
type approveGater struct {
	prompter GatePrompter
	logger   io.Writer
}

// PhaseGate renders the checkpoint and prompts for a decision.
func (g *approveGater) PhaseGate(ctx context.Context, _ *PhaseSpec, cp *Checkpoint) (GateAction, error) {
	if cp != nil {
		RenderCheckpoint(g.logger, cp)
	}
	action, err := g.prompter.Prompt(ctx, cp)
	if err != nil {
		fmt.Fprintf(g.logger, "warning: gate prompt failed: %v (defaulting to accept)\n", err)
		return GateActionAccept, nil
	}
	return action, nil
}

// PlanGate prompts for plan approval. Returns nil on accept, ErrPlanRejected otherwise.
func (g *approveGater) PlanGate(ctx context.Context, cp *Checkpoint) error {
	action, err := g.prompter.Prompt(ctx, cp)
	if err != nil {
		return fmt.Errorf("plan gate prompt failed: %w", err)
	}
	switch action {
	case GateActionAccept:
		return nil
	default:
		return ErrPlanRejected
	}
}

// compositeGater resolves the effective gate mode per-phase using manifest and
// phase-level overrides, then delegates to the appropriate strategy.
type compositeGater struct {
	execution  Execution          // manifest-level execution config
	strategies map[GateMode]Gater // mode → strategy
	fallback   Gater              // used when mode is unknown or empty
}

// PhaseGate resolves the per-phase gate mode and delegates to the corresponding strategy.
func (c *compositeGater) PhaseGate(ctx context.Context, phase *PhaseSpec, cp *Checkpoint) (GateAction, error) {
	mode := ResolveGate(c.execution, *phase)
	if g, ok := c.strategies[mode]; ok {
		return g.PhaseGate(ctx, phase, cp)
	}
	return c.fallback.PhaseGate(ctx, phase, cp)
}

// PlanGate delegates to the strategy for the manifest-level gate mode.
func (c *compositeGater) PlanGate(ctx context.Context, cp *Checkpoint) error {
	mode := c.execution.Gate
	if mode == "" {
		mode = GateModeTrust
	}
	if g, ok := c.strategies[mode]; ok {
		return g.PlanGate(ctx, cp)
	}
	return c.fallback.PlanGate(ctx, cp)
}

// NewGater returns a Gater that handles all four gate modes, resolving per-phase
// overrides via the manifest execution config. If prompter is nil, all modes that
// require user interaction fall back to trust behavior.
func NewGater(exec Execution, prompter GatePrompter, deps GaterDeps) Gater {
	logger := deps.Logger
	if logger == nil {
		logger = os.Stderr
	}

	trust := trustGater{}
	strategies := map[GateMode]Gater{
		GateModeTrust: trust,
		GateModeWatch: &watchGater{deps: deps},
	}

	if prompter != nil {
		strategies[GateModeReview] = &reviewGater{prompter: prompter, logger: logger}
		strategies[GateModeApprove] = &approveGater{prompter: prompter, logger: logger}
	} else {
		// No prompter: review/approve fall back to trust.
		strategies[GateModeReview] = trust
		strategies[GateModeApprove] = trust
	}

	return &compositeGater{
		execution:  exec,
		strategies: strategies,
		fallback:   trust,
	}
}

// --- Terminal GatePrompter implementation ---

// terminalGater reads gate decisions from stdin and writes prompts to stderr.
type terminalGater struct {
	in       io.Reader
	out      io.Writer
	forceTTY *bool // override isTTY check for testing; nil = auto-detect
}

// NewTerminalGater creates a GatePrompter that reads from stdin and writes to stderr.
func NewTerminalGater() GatePrompter {
	return &terminalGater{in: os.Stdin, out: os.Stderr}
}

// newTerminalGaterWithIO creates a GatePrompter with injectable I/O for testing.
func newTerminalGaterWithIO(in io.Reader, out io.Writer) GatePrompter {
	return &terminalGater{in: in, out: out}
}

// isTTY reports whether the reader is connected to a terminal.
func isTTY(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

// isTTYInput reports whether the gater's input is from a terminal.
// Uses the forceTTY override if set, otherwise auto-detects.
func (g *terminalGater) isTTYInput() bool {
	if g.forceTTY != nil {
		return *g.forceTTY
	}
	return isTTY(g.in)
}

// Prompt displays a gate prompt and waits for user input.
// In non-TTY environments, it defaults to accept with a warning.
// If the context is canceled, it returns GateActionSkip.
func (g *terminalGater) Prompt(ctx context.Context, cp *Checkpoint) (GateAction, error) {
	// Non-TTY: auto-accept with warning.
	if !g.isTTYInput() {
		phaseID := "unknown"
		if cp != nil {
			phaseID = cp.PhaseID
		}
		fmt.Fprintf(g.out, "warning: non-TTY stdin, auto-accepting gate for phase %q\n", phaseID)
		return GateActionAccept, nil
	}

	if cp != nil && cp.PhaseID == PlanPhaseID {
		fmt.Fprintf(g.out, "\n   [a]pprove  [s]kip (abort)\n   > ")
	} else {
		fmt.Fprintf(g.out, "\n   [a]ccept  [r]eject  re[t]ry  [s]kip\n   > ")
	}

	// Read input in a goroutine so we can respect context cancellation.
	type result struct {
		action GateAction
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		scanner := bufio.NewScanner(g.in)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				ch <- result{err: fmt.Errorf("failed to read gate input: %w", err)}
			} else {
				// EOF — treat as skip.
				ch <- result{action: GateActionSkip}
			}
			return
		}
		ch <- result{action: parseGateInput(scanner.Text())}
	}()

	select {
	case <-ctx.Done():
		return GateActionSkip, nil
	case r := <-ch:
		return r.action, r.err
	}
}

// parseGateInput maps a single-character (or word) input to a GateAction.
func parseGateInput(input string) GateAction {
	s := strings.TrimSpace(strings.ToLower(input))
	switch s {
	case "a", "accept":
		return GateActionAccept
	case "r", "reject":
		return GateActionReject
	case "t", "retry":
		return GateActionRetry
	case "s", "skip":
		return GateActionSkip
	default:
		return GateActionAccept // default to accept for unrecognized input
	}
}
