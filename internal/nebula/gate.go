package nebula

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
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

// Gater handles human interaction at phase boundaries.
type Gater interface {
	// Prompt displays the checkpoint and waits for a human decision.
	// Returns the chosen action.
	Prompt(ctx context.Context, cp *Checkpoint) (GateAction, error)
}

// terminalGater reads gate decisions from stdin and writes prompts to stderr.
type terminalGater struct {
	in       io.Reader
	out      io.Writer
	forceTTY *bool // override isTTY check for testing; nil = auto-detect
}

// NewTerminalGater creates a Gater that reads from stdin and writes to stderr.
func NewTerminalGater() Gater {
	return &terminalGater{in: os.Stdin, out: os.Stderr}
}

// newTerminalGaterWithIO creates a Gater with injectable I/O for testing.
func newTerminalGaterWithIO(in io.Reader, out io.Writer) Gater {
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
