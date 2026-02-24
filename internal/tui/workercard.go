package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// WorkerCard holds live state for an active quasar (worker) processing a phase.
// Data is populated from existing MsgPhase* messages — no new message types required.
type WorkerCard struct {
	PhaseID    string   // phase being executed
	QuasarID   string   // worker identifier (e.g. "q-1")
	Cycle      int      // current cycle number
	MaxCycles  int      // maximum allowed cycles
	TokensUsed int      // cumulative tokens spent on this phase
	Claims     []string // file paths currently touched by this quasar
	Activity   string   // human-readable activity: "coding...", "reviewing..."
	AgentRole  string   // "coder" or "reviewer"
}

// workerCardMinWidth is the minimum width for a single worker card.
const workerCardMinWidth = 30

// workerCardMaxWidth is the maximum width for a single worker card.
const workerCardMaxWidth = 50

// RenderWorkerCards renders a horizontal (or vertical on narrow terminals) stack
// of worker cards for all active phases. Cards appear only when the board view
// is active and at least one phase is in the Running state.
func RenderWorkerCards(cards []*WorkerCard, termWidth int) string {
	if len(cards) == 0 {
		return ""
	}

	cardWidth := cardWidth(len(cards), termWidth)

	// On narrow terminals, stack vertically instead of horizontally.
	if termWidth < workerCardMinWidth*2 || len(cards) == 1 {
		return renderCardsVertical(cards, cardWidth)
	}
	return renderCardsHorizontal(cards, cardWidth, termWidth)
}

// cardWidth computes the width for each card given the number of cards and terminal width.
func cardWidth(numCards, termWidth int) int {
	if numCards <= 0 {
		return workerCardMinWidth
	}
	w := termWidth / numCards
	if w < workerCardMinWidth {
		w = workerCardMinWidth
	}
	if w > workerCardMaxWidth {
		w = workerCardMaxWidth
	}
	return w
}

// renderCardsHorizontal renders cards side-by-side, wrapping to a new row
// when the terminal width is exceeded.
func renderCardsHorizontal(cards []*WorkerCard, cw, termWidth int) string {
	var rows []string
	var row []string
	rowWidth := 0

	for _, card := range cards {
		rendered := card.View(cw)
		if rowWidth+cw > termWidth && len(row) > 0 {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, row...))
			row = nil
			rowWidth = 0
		}
		row = append(row, rendered)
		rowWidth += cw
	}
	if len(row) > 0 {
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, row...))
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// renderCardsVertical renders cards stacked vertically for narrow terminals.
func renderCardsVertical(cards []*WorkerCard, cw int) string {
	var parts []string
	for _, card := range cards {
		parts = append(parts, card.View(cw))
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// View renders a single worker card as a bordered box with live state information.
func (wc *WorkerCard) View(width int) string {
	// Border consumes 2 characters on each side.
	innerWidth := width - 4
	if innerWidth < 10 {
		innerWidth = 10
	}

	var b strings.Builder

	// Title: phase name in accent color.
	titleStyle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	title := TruncateWithEllipsis(wc.PhaseID, innerWidth)
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n")

	// Quasar ID in nebula color.
	qStyle := lipgloss.NewStyle().Foreground(colorNebula)
	b.WriteString(qStyle.Render(wc.QuasarID))
	b.WriteString("\n")

	// Cycle counter.
	cycleLabel := fmt.Sprintf("cycle %d/%d", wc.Cycle, wc.MaxCycles)
	dimStyle := lipgloss.NewStyle().Foreground(colorMuted)
	b.WriteString(dimStyle.Render(cycleLabel))
	b.WriteString("\n")

	// Token spend.
	tokenLabel := fmt.Sprintf("tokens %d", wc.TokensUsed)
	b.WriteString(dimStyle.Render(tokenLabel))
	b.WriteString("\n")

	// Claims (file paths) — show up to 3, then "...".
	if len(wc.Claims) > 0 {
		claimStyle := lipgloss.NewStyle().Foreground(colorMutedLight)
		maxClaims := 3
		for i, fp := range wc.Claims {
			if i >= maxClaims {
				b.WriteString(claimStyle.Render(fmt.Sprintf("  +%d more", len(wc.Claims)-maxClaims)))
				b.WriteString("\n")
				break
			}
			truncated := TruncateWithEllipsis(fp, innerWidth-2)
			b.WriteString(claimStyle.Render("  " + truncated))
			b.WriteString("\n")
		}
	}

	// Activity line with role-appropriate color.
	activityColor := colorPrimary
	if wc.AgentRole == "reviewer" {
		activityColor = colorReviewer
	}
	actStyle := lipgloss.NewStyle().Foreground(activityColor)
	activity := wc.Activity
	if activity == "" {
		activity = activityFromRole(wc.AgentRole)
	}
	b.WriteString(actStyle.Render(activity))

	// Wrap in a rounded border box.
	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorMuted).
		Width(width-2). // account for border width
		Padding(0, 1)

	return cardStyle.Render(b.String())
}

// activityFromRole returns a default activity string based on the agent role.
func activityFromRole(role string) string {
	switch role {
	case "coder":
		return "coding..."
	case "reviewer":
		return "reviewing..."
	default:
		return "working..."
	}
}

// ActiveWorkerCards extracts worker cards for all phases currently in the Running state.
// The cards are ordered by quasar ID for stable rendering.
func ActiveWorkerCards(cards map[string]*WorkerCard) []*WorkerCard {
	if len(cards) == 0 {
		return nil
	}

	var active []*WorkerCard
	for _, card := range cards {
		active = append(active, card)
	}

	// Sort by QuasarID for stable ordering.
	sortWorkerCards(active)
	return active
}

// sortWorkerCards sorts cards by QuasarID lexicographically.
func sortWorkerCards(cards []*WorkerCard) {
	for i := 1; i < len(cards); i++ {
		for j := i; j > 0 && cards[j].QuasarID < cards[j-1].QuasarID; j-- {
			cards[j], cards[j-1] = cards[j-1], cards[j]
		}
	}
}
