package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/papapumpkin/quasar/internal/nebula"
)

// GateOption represents one selectable action in the gate prompt.
type GateOption struct {
	Label  string
	Action nebula.GateAction
}

// GatePrompt renders an overlay for gate decisions with checkpoint details.
type GatePrompt struct {
	PhaseID    string
	Options    []GateOption
	Cursor     int
	ResponseCh chan<- nebula.GateAction
	IsPlan     bool
	Width      int
	Height     int // available terminal height for scroll clamping

	// Checkpoint data rendered in the overlay.
	PhaseTitle       string
	ReviewSummary    string
	NeedsHumanReview bool
	Satisfaction     string
	Risk             string
	FilesChanged     []nebula.FileChange
	ReviewCycles     int
	CostUSD          float64

	ScrollOffset int // vertical scroll position within the detail body
}

// NewGatePrompt creates a gate prompt for the given checkpoint.
func NewGatePrompt(cp *nebula.Checkpoint, responseCh chan<- nebula.GateAction) *GatePrompt {
	isPlan := cp != nil && cp.PhaseID == nebula.PlanPhaseID
	phaseID := "unknown"
	if cp != nil {
		phaseID = cp.PhaseID
	}

	var options []GateOption
	if isPlan {
		options = []GateOption{
			{Label: "[a]ccept", Action: nebula.GateActionAccept},
			{Label: "[s]kip", Action: nebula.GateActionSkip},
		}
	} else {
		options = []GateOption{
			{Label: "[a]ccept", Action: nebula.GateActionAccept},
			{Label: "[x] reject", Action: nebula.GateActionReject},
			{Label: "[r]etry", Action: nebula.GateActionRetry},
			{Label: "[s]kip", Action: nebula.GateActionSkip},
		}
	}

	g := &GatePrompt{
		PhaseID:    phaseID,
		Options:    options,
		ResponseCh: responseCh,
		IsPlan:     isPlan,
	}

	if cp != nil {
		g.PhaseTitle = cp.PhaseTitle
		g.ReviewSummary = cp.ReviewSummary
		g.NeedsHumanReview = cp.NeedsHumanReview
		g.Satisfaction = cp.Satisfaction
		g.Risk = cp.Risk
		g.FilesChanged = cp.FilesChanged
		g.ReviewCycles = cp.ReviewCycles
		g.CostUSD = cp.CostUSD
	}

	return g
}

// Resolve sends the selected action and closes the response channel.
func (g *GatePrompt) Resolve(action nebula.GateAction) {
	if g.ResponseCh != nil {
		g.ResponseCh <- action
	}
}

// MoveLeft moves cursor left.
func (g *GatePrompt) MoveLeft() {
	if g.Cursor > 0 {
		g.Cursor--
	}
}

// MoveRight moves cursor right.
func (g *GatePrompt) MoveRight() {
	if g.Cursor < len(g.Options)-1 {
		g.Cursor++
	}
}

// ScrollUp scrolls the detail body up.
func (g *GatePrompt) ScrollUp() {
	if g.ScrollOffset > 0 {
		g.ScrollOffset--
	}
}

// ScrollDown scrolls the detail body down within the available content.
func (g *GatePrompt) ScrollDown(contentLines int, viewportLines int) {
	maxOffset := contentLines - viewportLines
	if maxOffset < 0 {
		maxOffset = 0
	}
	if g.ScrollOffset < maxOffset {
		g.ScrollOffset++
	}
}

// viewportHeight returns the number of detail lines visible in the overlay.
func (g *GatePrompt) viewportHeight() int {
	h := g.Height - 6 // border + padding + options
	if h < 5 {
		h = 5
	}
	return h
}

// contentLineCount returns the total number of lines in the detail body.
func (g *GatePrompt) contentLineCount() int {
	return strings.Count(g.detailBody(), "\n") + 1
}

// detailBody builds the styled detail section (everything above the option bar).
func (g *GatePrompt) detailBody() string {
	var b strings.Builder

	// Header with phase title.
	title := g.PhaseID
	if g.PhaseTitle != "" {
		title = g.PhaseTitle + " (" + g.PhaseID + ")"
	}
	b.WriteString(styleGateAction.Render(fmt.Sprintf("Gate: %s", title)))
	b.WriteString("\n")

	// Human review alert — prominent banner.
	if g.NeedsHumanReview {
		b.WriteString("\n" + styleGateHumanReview.Render(" HUMAN REVIEW REQUIRED ") + "\n")
	}

	// Status line: cycles, cost, satisfaction, risk.
	var statusParts []string
	if g.ReviewCycles > 0 {
		cycles := "cycle"
		if g.ReviewCycles != 1 {
			cycles = "cycles"
		}
		statusParts = append(statusParts, fmt.Sprintf("%d %s", g.ReviewCycles, cycles))
	}
	if g.CostUSD > 0 {
		statusParts = append(statusParts, fmt.Sprintf("$%.2f", g.CostUSD))
	}
	if g.Satisfaction != "" {
		statusParts = append(statusParts, "satisfaction: "+g.Satisfaction)
	}
	if g.Risk != "" {
		statusParts = append(statusParts, "risk: "+g.Risk)
	}
	if len(statusParts) > 0 {
		b.WriteString(styleGateDetail.Render(strings.Join(statusParts, "  ·  ")))
		b.WriteString("\n")
	}

	// Files changed.
	if len(g.FilesChanged) > 0 {
		b.WriteString("\n")
		b.WriteString(styleGateLabel.Render("Files:"))
		b.WriteString("\n")
		// Reserve space for border (4), padding (4), icon+spacing (4) = 12 chars.
		maxPathWidth := g.Width - 12
		if maxPathWidth < 20 {
			maxPathWidth = 20
		}
		for _, fc := range g.FilesChanged {
			icon := fileChangeIcon(fc.Operation)
			lineInfo := ""
			if fc.LinesAdded > 0 || fc.LinesRemoved > 0 {
				lineInfo = styleGateDetail.Render(fmt.Sprintf(" +%d -%d", fc.LinesAdded, fc.LinesRemoved))
			}
			path := fc.Path
			if len(path) > maxPathWidth && maxPathWidth > 3 {
				path = "..." + path[len(path)-maxPathWidth+3:]
			}
			b.WriteString(fmt.Sprintf("  %s %s%s\n", icon, path, lineInfo))
		}
	}

	// Reviewer summary.
	if g.ReviewSummary != "" {
		b.WriteString("\n")
		b.WriteString(styleGateLabel.Render("Reviewer:"))
		b.WriteString("\n")
		maxWidth := g.Width - 8
		if maxWidth < 40 {
			maxWidth = 60
		}
		b.WriteString("  " + wrapText(g.ReviewSummary, maxWidth))
		b.WriteString("\n")
	}

	return b.String()
}

// SelectedAction returns the currently highlighted action.
func (g *GatePrompt) SelectedAction() nebula.GateAction {
	if g.Cursor < 0 || g.Cursor >= len(g.Options) {
		return nebula.GateActionAccept
	}
	return g.Options[g.Cursor].Action
}

// View renders the gate prompt overlay with checkpoint details.
func (g GatePrompt) View() string {
	detail := g.detailBody()
	detailLines := strings.Split(detail, "\n")

	vpHeight := g.viewportHeight()
	if len(detailLines) > vpHeight {
		maxOffset := len(detailLines) - vpHeight
		if g.ScrollOffset > maxOffset {
			g.ScrollOffset = maxOffset
		}
		end := g.ScrollOffset + vpHeight
		if end > len(detailLines) {
			end = len(detailLines)
		}
		visible := detailLines[g.ScrollOffset:end]
		if g.ScrollOffset > 0 {
			visible = append([]string{styleGateDetail.Render("  ↑ scroll up")}, visible...)
		}
		if end < len(detailLines) {
			visible = append(visible, styleGateDetail.Render("  ↓ scroll down"))
		}
		detailLines = visible
	}

	var out strings.Builder
	out.WriteString(strings.Join(detailLines, "\n"))

	// Option bar.
	out.WriteString("\n\n")
	var optParts []string
	for i, opt := range g.Options {
		if i == g.Cursor {
			optParts = append(optParts, styleGateSelected.Render(opt.Label))
		} else {
			optParts = append(optParts, styleGateNormal.Render(opt.Label))
		}
	}
	out.WriteString(strings.Join(optParts, "  "))

	// Clamp overlay width to prevent spilling past the terminal edge.
	// Subtract 4 for the double border (2 chars each side).
	if g.Width > 0 {
		return styleGateOverlay.Width(g.Width - 4).Render(out.String())
	}
	return styleGateOverlay.Render(out.String())
}

// fileChangeIcon returns a colored icon for a file change operation.
func fileChangeIcon(operation string) string {
	switch operation {
	case "added":
		return lipgloss.NewStyle().Foreground(colorSuccess).Render("A")
	case "deleted":
		return lipgloss.NewStyle().Foreground(colorDanger).Render("D")
	default:
		return lipgloss.NewStyle().Foreground(colorAccent).Render("M")
	}
}
