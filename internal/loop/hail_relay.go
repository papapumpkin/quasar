package loop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
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
		switch {
		case h.Kind == HailGuidance:
			fmt.Fprintf(&b, "[HUMAN GUIDANCE]\nThe developer sent the following guidance for this phase:\n%q\nPlease incorporate this into your current work.\n\n", h.Resolution)
		case h.AutoResolved:
			fmt.Fprintf(&b, "[HAIL TIMEOUT] No response to your %s about %q (cycle %d). Proceed with your best judgment.\n\n", h.Kind, h.Summary, h.Cycle)
		default:
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
	// Check for guidance files first — these are human-initiated messages
	// that don't go through the HailQueue.
	guidanceBlock, guidanceIDs := l.consumeGuidanceFiles()

	if l.HailQueue == nil {
		return guidanceBlock, guidanceIDs
	}
	// Auto-resolve any hails that have exceeded the configured timeout.
	l.HailQueue.SweepExpired()

	hails := l.HailQueue.UnrelayedResolved()
	if len(hails) == 0 && guidanceBlock == "" {
		return "", nil
	}

	ids = make([]string, 0, len(hails)+len(guidanceIDs))
	for _, h := range hails {
		ids = append(ids, h.ID)
	}
	ids = append(ids, guidanceIDs...)

	// Combine guidance and hail relay blocks.
	hailBlock := formatHailRelay(hails)
	if guidanceBlock != "" && hailBlock != "" {
		return guidanceBlock + "\n" + hailBlock, ids
	}
	if guidanceBlock != "" {
		return guidanceBlock, ids
	}
	return hailBlock, ids
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

// consumeGuidanceFiles reads and removes GUIDANCE-<phaseID> files from
// GuidanceDir, posting each as a pre-resolved HailGuidance hail. This
// allows the TUI to inject human guidance into the agent's next cycle
// without requiring a direct channel to the HailQueue.
//
// Returns the formatted relay block and IDs for any guidance found, or
// empty values if no guidance files exist or GuidanceDir is empty.
func (l *Loop) consumeGuidanceFiles() (block string, ids []string) {
	if l.GuidanceDir == "" || l.PhaseID == "" {
		return "", nil
	}

	guidancePath := filepath.Join(l.GuidanceDir, fmt.Sprintf("GUIDANCE-%s", l.PhaseID))
	data, err := os.ReadFile(guidancePath)
	if err != nil {
		return "", nil // No guidance file — normal case.
	}

	// Remove the file immediately after reading to prevent re-processing.
	if removeErr := os.Remove(guidancePath); removeErr != nil {
		l.UI.Error(fmt.Sprintf("failed to remove guidance file: %v", removeErr))
	}

	guidance := strings.TrimSpace(string(data))
	if guidance == "" {
		return "", nil
	}

	// Create a pre-resolved guidance hail.
	hailID := fmt.Sprintf("guidance-%s-%d", l.PhaseID, time.Now().UnixMilli())
	h := Hail{
		ID:         hailID,
		PhaseID:    l.PhaseID,
		Kind:       HailGuidance,
		Summary:    "developer guidance",
		Resolution: guidance,
		SourceRole: "human",
		CreatedAt:  time.Now(),
		ResolvedAt: time.Now(),
	}

	// Format as a relay block (same format as formatHailRelay).
	hails := []Hail{h}
	return formatHailRelay(hails), []string{hailID}
}
