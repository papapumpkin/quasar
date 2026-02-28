package loop

import (
	"fmt"
	"strings"
)

// formatHailRelay formats resolved-but-unrelayed hails into a concise prompt
// block suitable for injection into coder or reviewer prompts. Auto-resolved
// hails (timed out without human response) use a distinct [HAIL TIMEOUT]
// header to signal the agent should proceed with its best judgment. Returns
// an empty string when there are no hails to relay.
func formatHailRelay(hails []Hail) string {
	if len(hails) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("[HUMAN RESPONSES]\n")
	for _, h := range hails {
		if h.AutoResolved {
			fmt.Fprintf(&b, "[HAIL TIMEOUT] No response to your %s about %q (cycle %d). Proceed with your best judgment.\n\n", h.Kind, h.Summary, h.Cycle)
		} else {
			fmt.Fprintf(&b, "Your %s about %q (cycle %d) was answered:\n", h.Kind, h.Summary, h.Cycle)
			fmt.Fprintf(&b, "%q\n\n", h.Resolution)
		}
	}
	b.WriteString("Proceed with this guidance in mind.\n")
	return b.String()
}

// pendingHailRelay queries the HailQueue for resolved-but-unrelayed hails,
// formats them into a prompt block, and returns both the block and the IDs
// that should be marked as relayed after the agent processes them. When no
// HailQueue is configured or no hails are pending, both return values are empty.
//
// Before checking for unrelayed resolutions, it sweeps expired hails so that
// timed-out hails are auto-resolved and included in the relay.
func (l *Loop) pendingHailRelay() (block string, ids []string) {
	if l.HailQueue == nil {
		return "", nil
	}
	// Auto-resolve any hails that have exceeded the configured timeout.
	l.HailQueue.SweepExpired()

	hails := l.HailQueue.UnrelayedResolved()
	if len(hails) == 0 {
		return "", nil
	}
	ids = make([]string, len(hails))
	for i, h := range hails {
		ids[i] = h.ID
	}
	return formatHailRelay(hails), ids
}

// markHailsRelayed marks the given hail IDs as relayed. Errors are logged
// via the UI but do not interrupt the loop — relay is best-effort.
func (l *Loop) markHailsRelayed(ids []string) {
	if l.HailQueue == nil || len(ids) == 0 {
		return
	}
	if err := l.HailQueue.MarkRelayed(ids); err != nil {
		l.UI.Error(fmt.Sprintf("failed to mark hails as relayed: %v", err))
	}
}
