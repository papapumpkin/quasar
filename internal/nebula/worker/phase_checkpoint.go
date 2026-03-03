package nebula

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/papapumpkin/quasar/internal/ansi"
)

// PlanPhaseID is the synthetic phase ID used for plan-level gate checkpoints.
const PlanPhaseID = "_plan"

// Checkpoint captures the outcome of a completed phase for human review.
type Checkpoint struct {
	PhaseID          string
	PhaseTitle       string
	NebulaName       string
	Status           PhaseStatus
	ReviewCycles     int
	CostUSD          float64
	ReviewSummary    string       // From ReviewReport.Summary
	NeedsHumanReview bool         // Reviewer flagged requirements-level issues
	Satisfaction     string       // Reviewer satisfaction level (high, medium, low)
	Risk             string       // Reviewer risk assessment (high, medium, low)
	Diff             string       // Output of git diff (the phase's commit vs prior)
	FilesChanged     []FileChange // Parsed summary of changed files
	BaseCommitSHA    string       // HEAD at start of the phase (empty if unavailable)
	FinalCommitSHA   string       // Last cycle's sealed SHA (empty if unavailable)
}

// FileChange summarizes a single file's changes within a phase commit.
type FileChange struct {
	Path         string
	Operation    string // "added", "modified", "deleted"
	LinesAdded   int
	LinesRemoved int
}

// BuildCheckpoint constructs a Checkpoint from the result of a completed phase.
// When both BaseCommitSHA and FinalCommitSHA are available in the result, it
// uses DiffRange to capture the full phase diff across all cycles. Otherwise it
// falls back to DiffLastCommit for the most recent commit only.
func BuildCheckpoint(ctx context.Context, git GitCommitter, phaseID string, result PhaseRunnerResult, nebula *Nebula) (*Checkpoint, error) {
	cp := &Checkpoint{
		PhaseID:        phaseID,
		NebulaName:     nebula.Manifest.Nebula.Name,
		Status:         PhaseStatusDone,
		ReviewCycles:   result.CyclesUsed,
		CostUSD:        result.TotalCostUSD,
		BaseCommitSHA:  result.BaseCommitSHA,
		FinalCommitSHA: result.FinalCommitSHA,
	}

	// Look up the phase title from the nebula spec.
	if p, ok := PhasesByID(nebula.Phases)[phaseID]; ok {
		cp.PhaseTitle = p.Title
	}

	// Populate review fields from the report.
	if result.Report != nil {
		cp.ReviewSummary = result.Report.Summary
		cp.NeedsHumanReview = result.Report.NeedsHumanReview
		cp.Satisfaction = result.Report.Satisfaction
		cp.Risk = result.Report.Risk
	}

	// Retrieve the diff and stat for the phase.
	// Prefer the full range (base..final) when both SHAs are available;
	// fall back to the last-commit diff otherwise.
	if git != nil {
		diff, stat, err := buildCheckpointDiffs(ctx, git, result.BaseCommitSHA, result.FinalCommitSHA)
		if err != nil {
			return nil, err
		}
		cp.Diff = diff
		cp.FilesChanged = ParseDiffStat(stat)
	}

	return cp, nil
}

// buildCheckpointDiffs returns the diff and stat output for a phase.
// When both base and final SHAs are provided, DiffRange/DiffStatRange are used
// to capture the full phase diff. Otherwise DiffLastCommit/DiffStatLastCommit
// provide the single-commit fallback.
func buildCheckpointDiffs(ctx context.Context, git GitCommitter, base, final string) (diff, stat string, err error) {
	if base != "" && final != "" {
		diff, err = git.DiffRange(ctx, base, final)
		if err != nil {
			return "", "", fmt.Errorf("failed to get phase diff range: %w", err)
		}
		stat, err = git.DiffStatRange(ctx, base, final)
		if err != nil {
			return "", "", fmt.Errorf("failed to get phase diff stat range: %w", err)
		}
		return diff, stat, nil
	}

	diff, err = git.DiffLastCommit(ctx)
	if err != nil {
		return "", "", fmt.Errorf("failed to get phase diff: %w", err)
	}
	stat, err = git.DiffStatLastCommit(ctx)
	if err != nil {
		return "", "", fmt.Errorf("failed to get phase diff stat: %w", err)
	}
	return diff, stat, nil
}

// ParseDiffStat parses git diff --stat output into FileChange entries.
// Each line has the form: " path/to/file | N ++--" or similar.
func ParseDiffStat(stat string) []FileChange {
	var changes []FileChange
	lines := strings.Split(stat, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// The summary line looks like " N files changed, X insertions(+), Y deletions(-)"
		if strings.Contains(line, "files changed") || strings.Contains(line, "file changed") {
			continue
		}
		fc := parseDiffStatLine(line)
		if fc != nil {
			changes = append(changes, *fc)
		}
	}
	return changes
}

// parseDiffStatLine parses a single line from git diff --stat output.
// Example: " scripts/test.sh | 15 +++++++++++++++"
// Example: " old.txt         |  0"
// Example: " gone.txt        |  8 --------"
func parseDiffStatLine(line string) *FileChange {
	// Split on "|" — the path is before, the stats are after.
	parts := strings.SplitN(line, "|", 2)
	if len(parts) != 2 {
		return nil
	}

	path := strings.TrimSpace(parts[0])
	if path == "" {
		return nil
	}

	statPart := strings.TrimSpace(parts[1])

	var added, removed int
	// Count "+" and "-" characters in the stat visualization.
	for _, c := range statPart {
		switch c {
		case '+':
			added++
		case '-':
			removed++
		}
	}

	// Also try to parse the numeric count for cases like "0" (rename with no changes).
	numStr := strings.Fields(statPart)
	if len(numStr) > 0 {
		if n, err := strconv.Atoi(numStr[0]); err == nil && n == 0 && added == 0 && removed == 0 {
			// File with 0 changes (e.g., mode change or rename).
			return &FileChange{
				Path:      path,
				Operation: "modified",
			}
		}
	}

	// Determine operation from the change pattern.
	op := "modified"
	if added > 0 && removed == 0 {
		op = "added"
	} else if removed > 0 && added == 0 {
		op = "deleted"
	}

	return &FileChange{
		Path:         path,
		Operation:    op,
		LinesAdded:   added,
		LinesRemoved: removed,
	}
}

// RenderCheckpoint writes a formatted checkpoint summary to the given writer.
// Output uses ANSI colors consistent with ui.Printer patterns.
func RenderCheckpoint(w io.Writer, cp *Checkpoint) {
	separator := ansi.Dim + "───────────────────────────────────────────────────" + ansi.Reset

	// Header with phase ID.
	title := cp.PhaseID
	if cp.PhaseTitle != "" {
		title = cp.PhaseTitle + " (" + cp.PhaseID + ")"
	}
	fmt.Fprintf(w, "\n"+ansi.Bold+ansi.Magenta+"── Phase: %s ──"+ansi.Reset+"\n", title)

	// Status line with review cycles and cost.
	statusStr := string(cp.Status)
	if cp.ReviewCycles > 0 || cp.CostUSD > 0 {
		var parts []string
		if cp.ReviewCycles > 0 {
			cycles := "cycle"
			if cp.ReviewCycles != 1 {
				cycles = "cycles"
			}
			parts = append(parts, fmt.Sprintf("%d review %s", cp.ReviewCycles, cycles))
		}
		if cp.CostUSD > 0 {
			parts = append(parts, fmt.Sprintf("$%.2f", cp.CostUSD))
		}
		statusStr += " (" + strings.Join(parts, ", ") + ")"
	}
	fmt.Fprintf(w, "   "+ansi.Dim+"Status:"+ansi.Reset+"  %s\n", statusStr)

	// Files changed.
	if len(cp.FilesChanged) > 0 {
		fmt.Fprintf(w, "   "+ansi.Dim+"Files:"+ansi.Reset+"\n")
		for _, fc := range cp.FilesChanged {
			icon, color := fileChangeStyle(fc.Operation)
			lineInfo := formatLineInfo(fc)
			fmt.Fprintf(w, "     %s%s %-40s%s %s\n", color, icon, fc.Path, ansi.Reset, lineInfo)
		}
	}

	// Reviewer summary.
	if cp.ReviewSummary != "" {
		fmt.Fprintf(w, "   "+ansi.Dim+"Reviewer:"+ansi.Reset+" %q\n", cp.ReviewSummary)
	}

	fmt.Fprintln(w, separator)
}

// fileChangeStyle returns the icon prefix and ANSI color for a file operation.
func fileChangeStyle(op string) (icon, color string) {
	switch op {
	case "added":
		return "+ ", ansi.Green
	case "deleted":
		return "- ", ansi.Red
	case "modified":
		return "~ ", ansi.Yellow
	default:
		return "  ", ""
	}
}

// formatLineInfo returns a human-readable summary of lines added/removed.
func formatLineInfo(fc FileChange) string {
	total := fc.LinesAdded + fc.LinesRemoved
	if total == 0 {
		return ""
	}
	var parts []string
	if fc.LinesAdded > 0 {
		parts = append(parts, fmt.Sprintf(ansi.Green+"+%d"+ansi.Reset, fc.LinesAdded))
	}
	if fc.LinesRemoved > 0 {
		parts = append(parts, fmt.Sprintf(ansi.Red+"-%d"+ansi.Reset, fc.LinesRemoved))
	}
	return "(" + strings.Join(parts, ", ") + ")"
}
