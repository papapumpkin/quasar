package tui

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/papapumpkin/quasar/internal/ui"
)

// gitDiffTimeout is the maximum time allowed for a git diff subprocess.
const gitDiffTimeout = 10 * time.Second

// UIBridge implements ui.UI by forwarding each call as a typed message
// to a BubbleTea program. tea.Program.Send is goroutine-safe, so multiple
// loop/nebula workers can call the bridge concurrently.
// Used in single-task (loop) mode where there is no phase context.
type UIBridge struct {
	program *tea.Program
	workDir string // working directory for git diff capture
	cycle   int    // current cycle number, set by CycleStart
}

// Verify UIBridge satisfies ui.UI at compile time.
var _ ui.UI = (*UIBridge)(nil)

// NewUIBridge creates a bridge that sends messages to the given program.
// The workDir is used to run git diff after coder agents complete.
func NewUIBridge(p *tea.Program, workDir string) *UIBridge {
	return &UIBridge{program: p, workDir: workDir}
}

// TaskStarted sends MsgTaskStarted.
func (b *UIBridge) TaskStarted(beadID, title string) {
	b.program.Send(MsgTaskStarted{BeadID: beadID, Title: title})
}

// TaskComplete sends MsgTaskComplete.
func (b *UIBridge) TaskComplete(beadID string, totalCost float64) {
	b.program.Send(MsgTaskComplete{BeadID: beadID, TotalCost: totalCost})
}

// CycleStart sends MsgCycleStart and records the current cycle number.
func (b *UIBridge) CycleStart(cycle, maxCycles int) {
	b.cycle = cycle
	b.program.Send(MsgCycleStart{Cycle: cycle, MaxCycles: maxCycles})
}

// AgentStart sends MsgAgentStart.
func (b *UIBridge) AgentStart(role string) {
	b.program.Send(MsgAgentStart{Role: role})
}

// AgentDone sends MsgAgentDone. For coder agents, it also captures the git
// diff of the most recent commit and sends MsgAgentDiff.
func (b *UIBridge) AgentDone(role string, costUSD float64, durationMs int64) {
	b.program.Send(MsgAgentDone{Role: role, CostUSD: costUSD, DurationMs: durationMs})
	if role == "coder" {
		if dr := captureGitDiff(b.workDir, "", ""); dr.Diff != "" {
			b.program.Send(MsgAgentDiff{
				Role:    role,
				Cycle:   b.cycle,
				Diff:    dr.Diff,
				BaseRef: dr.BaseRef,
				HeadRef: dr.HeadRef,
				Files:   dr.Files,
				WorkDir: b.workDir,
			})
		}
	}
}

// CycleSummary sends MsgCycleSummary.
func (b *UIBridge) CycleSummary(d ui.CycleSummaryData) {
	b.program.Send(MsgCycleSummary{Data: d})
}

// IssuesFound sends MsgIssuesFound.
func (b *UIBridge) IssuesFound(count int) {
	b.program.Send(MsgIssuesFound{Count: count})
}

// Approved sends MsgApproved.
func (b *UIBridge) Approved() {
	b.program.Send(MsgApproved{})
}

// MaxCyclesReached sends MsgMaxCyclesReached.
func (b *UIBridge) MaxCyclesReached(max int) {
	b.program.Send(MsgMaxCyclesReached{Max: max})
}

// BudgetExceeded sends MsgBudgetExceeded.
func (b *UIBridge) BudgetExceeded(spent, limit float64) {
	b.program.Send(MsgBudgetExceeded{Spent: spent, Limit: limit})
}

// Error sends MsgError.
func (b *UIBridge) Error(msg string) {
	b.program.Send(MsgError{Msg: msg})
}

// Info sends MsgInfo.
func (b *UIBridge) Info(msg string) {
	b.program.Send(MsgInfo{Msg: msg})
}

// AgentOutput sends MsgAgentOutput for drill-down display.
func (b *UIBridge) AgentOutput(role string, cycle int, output string) {
	b.program.Send(MsgAgentOutput{Role: role, Cycle: cycle, Output: output})
}

// RefactorApplied is a no-op for the single-task UIBridge; refactor indicators
// are only meaningful in nebula phase views where PhaseUIBridge is used.
func (b *UIBridge) RefactorApplied(phaseID string) {}

// HailReceived sends MsgHailReceived when an agent posts a hail.
func (b *UIBridge) HailReceived(h ui.HailInfo) {
	b.program.Send(MsgHailReceived{Hail: h})
}

// HailResolved sends MsgHailResolved when a hail is resolved by the human.
func (b *UIBridge) HailResolved(id, resolution string) {
	b.program.Send(MsgHailResolved{ID: id, Resolution: resolution})
}

// BeadUpdate sends MsgBeadUpdate with the bead hierarchy.
func (b *UIBridge) BeadUpdate(taskBeadID, title, status string, children []ui.BeadChild) {
	root := buildBeadInfoTree(taskBeadID, title, status, children)
	b.program.Send(MsgBeadUpdate{TaskBeadID: taskBeadID, Root: root})
}

// buildBeadInfoTree converts a task bead and its children into a BeadInfo tree.
// Used by both UIBridge and PhaseUIBridge to avoid duplicated conversion logic.
func buildBeadInfoTree(taskBeadID, title, status string, children []ui.BeadChild) BeadInfo {
	root := BeadInfo{
		ID:     taskBeadID,
		Title:  title,
		Status: status,
		Type:   "task",
	}
	for _, c := range children {
		root.Children = append(root.Children, BeadInfo{
			ID:       c.ID,
			Title:    c.Title,
			Status:   c.Status,
			Type:     "bug",
			Severity: c.Severity,
			Cycle:    c.Cycle,
		})
	}
	return root
}

// diffResult holds the raw unified diff and pre-parsed structured metadata
// returned by captureGitDiff.
type diffResult struct {
	Diff    string
	BaseRef string
	HeadRef string
	Files   []FileStatEntry
}

// captureGitDiff runs "git diff <base>..<head>" in the given directory and
// returns both the raw unified diff and pre-parsed file stats. When baseRef or
// headRef are empty it falls back to HEAD~1..HEAD. Returns a zero diffResult on
// any error (no git repo, no commits, command failure) — diff capture is
// best-effort.
func captureGitDiff(workDir, baseRef, headRef string) diffResult {
	if workDir == "" {
		return diffResult{}
	}

	// Fall back to HEAD~1..HEAD when refs are not provided.
	if baseRef == "" || headRef == "" {
		baseRef = "HEAD~1"
		headRef = "HEAD"
	}

	refRange := baseRef + ".." + headRef
	ctx, cancel := context.WithTimeout(context.Background(), gitDiffTimeout)
	defer cancel()

	// Capture raw unified diff.
	cmd := exec.CommandContext(ctx, "git", "diff", refRange)
	cmd.Dir = workDir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil // discard stderr
	if err := cmd.Run(); err != nil {
		return diffResult{}
	}
	rawDiff := out.String()
	if rawDiff == "" {
		return diffResult{}
	}

	// Parse file stats from git diff --stat.
	files := captureGitDiffStat(workDir, refRange)

	return diffResult{
		Diff:    rawDiff,
		BaseRef: baseRef,
		HeadRef: headRef,
		Files:   files,
	}
}

// captureGitDiffStat runs "git diff --stat <refRange>" and parses the per-file
// stats into FileStatEntry slices. Returns nil on any error.
func captureGitDiffStat(workDir, refRange string) []FileStatEntry {
	ctx, cancel := context.WithTimeout(context.Background(), gitDiffTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "diff", "--numstat", refRange)
	cmd.Dir = workDir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return nil
	}
	return parseNumstat(out.String())
}

// parseNumstat parses the output of "git diff --numstat" into FileStatEntry
// slices. Each line has the format: <additions>\t<deletions>\t<path>.
// Binary files show "-" for additions/deletions and are recorded as zero.
func parseNumstat(output string) []FileStatEntry {
	var entries []FileStatEntry
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		adds, _ := strconv.Atoi(parts[0]) // "-" for binary → 0
		dels, _ := strconv.Atoi(parts[1])
		entries = append(entries, FileStatEntry{
			Path:      parts[2],
			Additions: adds,
			Deletions: dels,
		})
	}
	return entries
}
