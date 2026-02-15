package nebula

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ANSI color codes for checkpoint rendering.
const (
	cpReset   = "\033[0m"
	cpBold    = "\033[1m"
	cpDim     = "\033[2m"
	cpGreen   = "\033[32m"
	cpYellow  = "\033[33m"
	cpRed     = "\033[31m"
	cpCyan    = "\033[36m"
	cpMagenta = "\033[35m"
)

// PlanPhaseID is the synthetic phase ID used for plan-level gate checkpoints.
const PlanPhaseID = "_plan"

// Checkpoint captures the outcome of a completed phase for human review.
type Checkpoint struct {
	PhaseID       string
	PhaseTitle    string
	NebulaName    string
	Status        PhaseStatus
	ReviewCycles  int
	CostUSD       float64
	ReviewSummary string       // From ReviewReport.Summary
	Diff          string       // Output of git diff (the phase's commit vs prior)
	FilesChanged  []FileChange // Parsed summary of changed files
}

// FileChange summarizes a single file's changes within a phase commit.
type FileChange struct {
	Path         string
	Operation    string // "added", "modified", "deleted"
	LinesAdded   int
	LinesRemoved int
}

// BuildCheckpoint constructs a Checkpoint from the result of a completed phase.
// It uses the GitCommitter to retrieve the diff of the most recent commit.
func BuildCheckpoint(ctx context.Context, git GitCommitter, phaseID string, result PhaseRunnerResult, nebula *Nebula) (*Checkpoint, error) {
	cp := &Checkpoint{
		PhaseID:      phaseID,
		NebulaName:   nebula.Manifest.Nebula.Name,
		Status:       PhaseStatusDone,
		ReviewCycles: result.CyclesUsed,
		CostUSD:      result.TotalCostUSD,
	}

	// Look up the phase title from the nebula spec.
	for _, p := range nebula.Phases {
		if p.ID == phaseID {
			cp.PhaseTitle = p.Title
			break
		}
	}

	// Populate review summary from the report.
	if result.Report != nil {
		cp.ReviewSummary = result.Report.Summary
	}

	// Retrieve the diff and stat for the last commit.
	if git != nil {
		diff, err := git.DiffLastCommit(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get phase diff: %w", err)
		}
		cp.Diff = diff

		stat, err := git.DiffStatLastCommit(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get phase diff stat: %w", err)
		}
		cp.FilesChanged = ParseDiffStat(stat)
	}

	return cp, nil
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
	separator := cpDim + "───────────────────────────────────────────────────" + cpReset

	// Header with phase ID.
	title := cp.PhaseID
	if cp.PhaseTitle != "" {
		title = cp.PhaseTitle + " (" + cp.PhaseID + ")"
	}
	fmt.Fprintf(w, "\n"+cpBold+cpMagenta+"── Phase: %s ──"+cpReset+"\n", title)

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
	fmt.Fprintf(w, "   "+cpDim+"Status:"+cpReset+"  %s\n", statusStr)

	// Files changed.
	if len(cp.FilesChanged) > 0 {
		fmt.Fprintf(w, "   "+cpDim+"Files:"+cpReset+"\n")
		for _, fc := range cp.FilesChanged {
			icon, color := fileChangeStyle(fc.Operation)
			lineInfo := formatLineInfo(fc)
			fmt.Fprintf(w, "     %s%s %-40s%s %s\n", color, icon, fc.Path, cpReset, lineInfo)
		}
	}

	// Reviewer summary.
	if cp.ReviewSummary != "" {
		fmt.Fprintf(w, "   "+cpDim+"Reviewer:"+cpReset+" %q\n", cp.ReviewSummary)
	}

	fmt.Fprintln(w, separator)
}

// fileChangeStyle returns the icon prefix and ANSI color for a file operation.
func fileChangeStyle(op string) (icon, color string) {
	switch op {
	case "added":
		return "+ ", cpGreen
	case "deleted":
		return "- ", cpRed
	case "modified":
		return "~ ", cpYellow
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
		parts = append(parts, fmt.Sprintf(cpGreen+"+%d"+cpReset, fc.LinesAdded))
	}
	if fc.LinesRemoved > 0 {
		parts = append(parts, fmt.Sprintf(cpRed+"-%d"+cpReset, fc.LinesRemoved))
	}
	return "(" + strings.Join(parts, ", ") + ")"
}
