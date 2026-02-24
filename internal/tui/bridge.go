package tui

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/papapumpkin/quasar/internal/fabric"
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

// PhaseUIBridge implements ui.UI by sending phase-contextualized messages.
// Each nebula phase gets its own PhaseUIBridge so messages carry the PhaseID.
type PhaseUIBridge struct {
	program *tea.Program
	phaseID string
	workDir string // working directory for git diff capture
	cycle   int    // current cycle number, set by CycleStart
}

// Verify PhaseUIBridge satisfies ui.UI at compile time.
var _ ui.UI = (*PhaseUIBridge)(nil)

// NewPhaseUIBridge creates a bridge tagged with a specific phase ID.
// The workDir is used to run git diff after coder agents complete.
func NewPhaseUIBridge(p *tea.Program, phaseID, workDir string) *PhaseUIBridge {
	return &PhaseUIBridge{program: p, phaseID: phaseID, workDir: workDir}
}

// TaskStarted sends MsgPhaseTaskStarted.
func (b *PhaseUIBridge) TaskStarted(beadID, title string) {
	b.program.Send(MsgPhaseTaskStarted{PhaseID: b.phaseID, BeadID: beadID, Title: title})
}

// TaskComplete sends MsgPhaseTaskComplete.
func (b *PhaseUIBridge) TaskComplete(beadID string, totalCost float64) {
	b.program.Send(MsgPhaseTaskComplete{PhaseID: b.phaseID, BeadID: beadID, TotalCost: totalCost})
}

// CycleStart sends MsgPhaseCycleStart and records the current cycle number.
func (b *PhaseUIBridge) CycleStart(cycle, maxCycles int) {
	b.cycle = cycle
	b.program.Send(MsgPhaseCycleStart{PhaseID: b.phaseID, Cycle: cycle, MaxCycles: maxCycles})
}

// AgentStart sends MsgPhaseAgentStart.
func (b *PhaseUIBridge) AgentStart(role string) {
	b.program.Send(MsgPhaseAgentStart{PhaseID: b.phaseID, Role: role})
}

// AgentDone sends MsgPhaseAgentDone. For coder agents, it also captures the
// git diff of the most recent commit and sends MsgPhaseAgentDiff.
func (b *PhaseUIBridge) AgentDone(role string, costUSD float64, durationMs int64) {
	b.program.Send(MsgPhaseAgentDone{PhaseID: b.phaseID, Role: role, CostUSD: costUSD, DurationMs: durationMs})
	if role == "coder" {
		if dr := captureGitDiff(b.workDir, "", ""); dr.Diff != "" {
			b.program.Send(MsgPhaseAgentDiff{
				PhaseID: b.phaseID,
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

// CycleSummary sends MsgPhaseCycleSummary.
func (b *PhaseUIBridge) CycleSummary(d ui.CycleSummaryData) {
	b.program.Send(MsgPhaseCycleSummary{PhaseID: b.phaseID, Data: d})
}

// IssuesFound sends MsgPhaseIssuesFound.
func (b *PhaseUIBridge) IssuesFound(count int) {
	b.program.Send(MsgPhaseIssuesFound{PhaseID: b.phaseID, Count: count})
}

// Approved sends MsgPhaseApproved.
func (b *PhaseUIBridge) Approved() {
	b.program.Send(MsgPhaseApproved{PhaseID: b.phaseID})
}

// MaxCyclesReached sends MsgPhaseError (treated as an error for the phase).
func (b *PhaseUIBridge) MaxCyclesReached(max int) {
	b.program.Send(MsgPhaseError{PhaseID: b.phaseID, Msg: fmt.Sprintf("max cycles reached (%d)", max)})
}

// BudgetExceeded sends MsgPhaseError.
func (b *PhaseUIBridge) BudgetExceeded(spent, limit float64) {
	b.program.Send(MsgPhaseError{PhaseID: b.phaseID, Msg: fmt.Sprintf("budget exceeded ($%.2f / $%.2f)", spent, limit)})
}

// Error sends MsgPhaseError.
func (b *PhaseUIBridge) Error(msg string) {
	b.program.Send(MsgPhaseError{PhaseID: b.phaseID, Msg: msg})
}

// Info sends MsgPhaseInfo.
func (b *PhaseUIBridge) Info(msg string) {
	b.program.Send(MsgPhaseInfo{PhaseID: b.phaseID, Msg: msg})
}

// AgentOutput sends MsgPhaseAgentOutput.
func (b *PhaseUIBridge) AgentOutput(role string, cycle int, output string) {
	b.program.Send(MsgPhaseAgentOutput{PhaseID: b.phaseID, Role: role, Cycle: cycle, Output: output})
}

// RefactorApplied sends MsgPhaseRefactorApplied to notify the TUI that the
// loop consumed the pending refactor for this phase.
func (b *PhaseUIBridge) RefactorApplied(phaseID string) {
	b.program.Send(MsgPhaseRefactorApplied{PhaseID: b.phaseID})
}

// BeadUpdate sends MsgPhaseBeadUpdate with the bead hierarchy for this phase.
func (b *PhaseUIBridge) BeadUpdate(taskBeadID, title, status string, children []ui.BeadChild) {
	root := buildBeadInfoTree(taskBeadID, title, status, children)
	b.program.Send(MsgPhaseBeadUpdate{PhaseID: b.phaseID, TaskBeadID: taskBeadID, Root: root})
}

// EntanglementPublished sends MsgEntanglementUpdate with the full entanglement list.
func (b *PhaseUIBridge) EntanglementPublished(entanglements []fabric.Entanglement) {
	b.program.Send(MsgEntanglementUpdate{Entanglements: entanglements})
}

// DiscoveryPosted sends MsgDiscoveryPosted when a new discovery is recorded.
func (b *PhaseUIBridge) DiscoveryPosted(d fabric.Discovery) {
	b.program.Send(MsgDiscoveryPosted{Discovery: d})
}

// Hail sends MsgHail when a phase requires human attention.
// The ResponseCh is nil here (fire-and-forget). Callers that need to block on
// the user's response should use HailAndWait instead, which creates a channel
// and blocks until the overlay resolves or the context is canceled.
func (b *PhaseUIBridge) Hail(phaseID string, d fabric.Discovery) {
	b.program.Send(MsgHail{PhaseID: phaseID, Discovery: d})
}

// HailAndWait sends MsgHail and blocks until the user responds or the context
// is canceled. Returns the user's free-text response.
func (b *PhaseUIBridge) HailAndWait(ctx context.Context, phaseID string, d fabric.Discovery) (string, error) {
	responseCh := make(chan string, 1)
	b.program.Send(MsgHail{PhaseID: phaseID, Discovery: d, ResponseCh: responseCh})

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case resp := <-responseCh:
		return resp, nil
	}
}

// ScratchpadNote sends MsgScratchpadEntry with a timestamped note.
func (b *PhaseUIBridge) ScratchpadNote(phaseID, text string) {
	b.program.Send(MsgScratchpadEntry{
		Timestamp: time.Now(),
		PhaseID:   phaseID,
		Text:      text,
	})
}
