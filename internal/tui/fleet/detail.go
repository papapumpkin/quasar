package fleet

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/papapumpkin/quasar/internal/gitops"
)

// gitStatusTimeout bounds the per-repo git status probe so a slow repo cannot
// stall the UI.
const gitStatusTimeout = 2 * time.Second

// RenderTrace renders a run's star-invocation trace as a deterministic,
// ANSI-free block for the detail view. An empty trace yields a placeholder.
func RenderTrace(trace []Invocation) string {
	if len(trace) == 0 {
		return "(no steps recorded yet)"
	}
	var b strings.Builder
	for _, inv := range trace {
		fmt.Fprintf(&b, "  %2d. %-10s %-16s %s%s\n",
			inv.Seq, inv.Node, inv.StarName, traceGlyph(inv.State), traceDuration(inv))
	}
	return strings.TrimRight(b.String(), "\n")
}

// traceGlyph maps an invocation state to a compact status marker.
func traceGlyph(state string) string {
	switch state {
	case "done":
		return "✓ done"
	case "failed":
		return "✗ failed"
	case "running":
		return "↻ running"
	default:
		return state
	}
}

// traceDuration formats an invocation's elapsed time when both ends are known.
func traceDuration(inv Invocation) string {
	if inv.Started.IsZero() || inv.Ended.IsZero() {
		return ""
	}
	return fmt.Sprintf(" (%s)", inv.Ended.Sub(inv.Started).Round(time.Second))
}

// gitSummaryContext returns a one-line porcelain summary (modified/untracked
// counts and branch ahead/behind) for a repo. It is informational only and
// never mutates the repo. Failures yield a short, non-fatal note. The per-repo
// timeout is derived from parent, so a quit cancels an in-flight probe. This
// MUST be called from a tea.Cmd, never from View() — it shells out to git.
func gitSummaryContext(parent context.Context, path string) string {
	ctx, cancel := context.WithTimeout(parent, gitStatusTimeout)
	defer cancel()

	out, err := gitops.New(path).Porcelain(ctx, true)
	if err != nil {
		return "unavailable"
	}
	return summarizePorcelain(out)
}

// summarizePorcelain reduces `git status --porcelain=v2 --branch` output to a
// compact summary string.
func summarizePorcelain(out string) string {
	var modified, untracked, ahead, behind int
	branch := "?"
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			branch = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.ab "):
			// Format is "+N -M"; on a malformed line report no divergence.
			if _, err := fmt.Sscanf(strings.TrimPrefix(line, "# branch.ab "), "+%d -%d", &ahead, &behind); err != nil {
				ahead, behind = 0, 0
			}
		case strings.HasPrefix(line, "? "):
			untracked++
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "):
			modified++
		}
	}
	return fmt.Sprintf("%s  ~%d  ?%d  ↑%d ↓%d", branch, modified, untracked, ahead, behind)
}
