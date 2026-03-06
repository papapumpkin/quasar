package tycho

import (
	"context"
	"fmt"
	"io"

	"github.com/papapumpkin/quasar/internal/dialog"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// EscalationResult is the human's decision for a blocked phase.
type EscalationResult int

const (
	EscalationFail  EscalationResult = iota // mark the phase as failed
	EscalationRetry                         // reset the phase to queued
	EscalationSkip                          // skip (same effect as fail for now)
)

// EscalationHandler manages interactive escalation dialogs for blocked
// phases. It wraps a dialog.Opener and translates human responses into
// typed results that the scheduler can act on.
type EscalationHandler struct {
	Dialog dialog.Opener
	Fabric fabric.Fabric
	Logger io.Writer
}

// Run opens an interactive dialog for a blocked phase and blocks until
// the human resolves it. Returns the human's decision.
func (h *EscalationHandler) Run(ctx context.Context, phaseID string, bp *fabric.BlockedPhase, maxRetries int) EscalationResult {
	sess, err := h.open(ctx, phaseID, bp, maxRetries)
	if err != nil {
		fmt.Fprintf(h.logger(), "warning: failed to open escalation dialog for %q: %v\n", phaseID, err)
		return EscalationFail
	}
	defer sess.Close()

	h.sendIntro(ctx, sess, phaseID, bp)
	return h.conversationLoop(ctx, sess)
}

func (h *EscalationHandler) open(ctx context.Context, phaseID string, bp *fabric.BlockedPhase, maxRetries int) (dialog.Session, error) {
	return h.Dialog.Open(ctx, dialog.Request{
		PhaseID: phaseID,
		Title:   fmt.Sprintf("Phase %q needs human decision", phaseID),
		Context: fabric.EscalationMessage(bp, maxRetries),
		Kind:    "escalation",
		Options: []string{"retry", "skip", "fail"},
	})
}

func (h *EscalationHandler) sendIntro(ctx context.Context, sess dialog.Session, phaseID string, bp *fabric.BlockedPhase) {
	_ = sess.Send(ctx, fmt.Sprintf(
		"Phase %q is blocked: %s\n\nChoose an option (a/b/c) or type guidance.",
		phaseID, bp.LastResult.Reason,
	))
}

func (h *EscalationHandler) conversationLoop(ctx context.Context, sess dialog.Session) EscalationResult {
	for {
		resp, err := sess.Receive(ctx)
		if err != nil {
			return EscalationFail
		}
		if result, resolved := parseEscalationResponse(resp); resolved {
			h.sendResolution(ctx, sess, result)
			return result
		}
		_ = sess.Send(ctx, "Noted. Continue adding context, or choose: a) retry  b) skip  c) fail")
	}
}

func (h *EscalationHandler) sendResolution(ctx context.Context, sess dialog.Session, result EscalationResult) {
	switch result {
	case EscalationRetry:
		_ = sess.Send(ctx, "Resetting phase to queued for retry.")
	case EscalationSkip:
		_ = sess.Send(ctx, "Skipping phase.")
	case EscalationFail:
		_ = sess.Send(ctx, "Marking phase as failed.")
	}
}

func (h *EscalationHandler) logger() io.Writer {
	if h.Logger != nil {
		return h.Logger
	}
	return io.Discard
}

// parseEscalationResponse maps a human response to a typed result.
// Returns (result, true) for recognized commands, (zero, false) for
// free-text that should continue the conversation.
func parseEscalationResponse(resp string) (EscalationResult, bool) {
	switch resp {
	case "retry", "a":
		return EscalationRetry, true
	case "skip", "b":
		return EscalationSkip, true
	case "fail", "c":
		return EscalationFail, true
	default:
		return 0, false
	}
}
