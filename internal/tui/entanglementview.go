package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// EntanglementView renders a scrollable list of entanglement cards grouped by
// producer phase. It consumes MsgEntanglementUpdate from the fabric bridge and
// displays each entanglement's ID, producer→consumer parties, status, and
// interface body as monospace code.
type EntanglementView struct {
	Entanglements []fabric.Entanglement
	Cursor        int
	Width         int
	Height        int
	ScrollOffset  int
}

// NewEntanglementView creates an empty entanglement view.
func NewEntanglementView() EntanglementView {
	return EntanglementView{}
}

// entanglementGroup holds a producer label and its sorted entanglements.
type entanglementGroup struct {
	Producer      string
	Entanglements []fabric.Entanglement
}

// groupEntanglements groups entanglements by producer, then sorts within each
// group by status priority: disputed first, then pending, then fulfilled.
func groupEntanglements(es []fabric.Entanglement) []entanglementGroup {
	if len(es) == 0 {
		return nil
	}

	// Collect by producer, preserving first-seen order.
	seen := make(map[string]int)
	var groups []entanglementGroup
	for _, e := range es {
		idx, ok := seen[e.Producer]
		if !ok {
			idx = len(groups)
			seen[e.Producer] = idx
			groups = append(groups, entanglementGroup{Producer: e.Producer})
		}
		groups[idx].Entanglements = append(groups[idx].Entanglements, e)
	}

	// Sort groups alphabetically by producer.
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Producer < groups[j].Producer
	})

	// Sort within each group by status priority.
	for i := range groups {
		sort.SliceStable(groups[i].Entanglements, func(a, b int) bool {
			return statusPriority(groups[i].Entanglements[a].Status) <
				statusPriority(groups[i].Entanglements[b].Status)
		})
	}

	return groups
}

// statusPriority returns a sort key for entanglement status.
// Lower values sort first: disputed (0), pending (1), fulfilled (2).
func statusPriority(status string) int {
	switch status {
	case fabric.StatusDisputed:
		return 0
	case fabric.StatusPending:
		return 1
	case fabric.StatusFulfilled:
		return 2
	default:
		return 3
	}
}

// statusColor returns the lipgloss color for an entanglement status.
func statusColor(status string) lipgloss.Color {
	switch status {
	case fabric.StatusDisputed:
		return colorDanger
	case fabric.StatusPending:
		return colorStarYellow
	case fabric.StatusFulfilled:
		return colorSuccess
	default:
		return colorMuted
	}
}

// flatIndex returns the overall index for an entanglement given the grouped
// layout. Returns -1 if the cursor exceeds the total count.
func flatCount(groups []entanglementGroup) int {
	n := 0
	for _, g := range groups {
		n += len(g.Entanglements)
	}
	return n
}

// MoveUp moves the cursor up by one entanglement.
func (ev *EntanglementView) MoveUp() {
	if ev.Cursor > 0 {
		ev.Cursor--
	}
}

// MoveDown moves the cursor down by one entanglement.
func (ev *EntanglementView) MoveDown() {
	groups := groupEntanglements(ev.Entanglements)
	max := flatCount(groups) - 1
	if max < 0 {
		max = 0
	}
	if ev.Cursor < max {
		ev.Cursor++
	}
}

// ClampCursor ensures the cursor is within bounds for the current entanglements.
func (ev *EntanglementView) ClampCursor() {
	groups := groupEntanglements(ev.Entanglements)
	total := flatCount(groups)
	if total == 0 {
		ev.Cursor = 0
		return
	}
	if ev.Cursor >= total {
		ev.Cursor = total - 1
	}
	if ev.Cursor < 0 {
		ev.Cursor = 0
	}
}

// View renders the entanglement view with grouped, bordered cards.
func (ev EntanglementView) View() string {
	if len(ev.Entanglements) == 0 {
		return lipgloss.NewStyle().
			Foreground(colorMuted).
			PaddingLeft(2).
			Render("No entanglements")
	}

	groups := groupEntanglements(ev.Entanglements)
	cardWidth := ev.Width - 4
	if cardWidth < 20 {
		cardWidth = 20
	}

	var sb strings.Builder
	flatIdx := 0

	for gi, group := range groups {
		// Section header for producer group.
		header := fmt.Sprintf("  ◆ %s", group.Producer)
		headerStyle := lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)
		sb.WriteString(headerStyle.Render(header))
		sb.WriteString("\n")

		for _, ent := range group.Entanglements {
			selected := flatIdx == ev.Cursor
			sb.WriteString(ev.renderCard(ent, selected, cardWidth))
			sb.WriteString("\n")
			flatIdx++
		}

		// Add a blank line between groups (but not after the last one).
		if gi < len(groups)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// renderCard renders a single entanglement as a bordered card.
func (ev EntanglementView) renderCard(ent fabric.Entanglement, selected bool, cardWidth int) string {
	innerWidth := cardWidth - 4 // border padding
	if innerWidth < 16 {
		innerWidth = 16
	}

	// Title line: entanglement ID.
	idStyle := lipgloss.NewStyle().
		Foreground(colorBlueshift).
		Bold(true)
	title := idStyle.Render(fmt.Sprintf("#%d %s", ent.ID, ent.Name))

	// Parties line: producer → consumer.
	consumer := ent.Consumer
	if consumer == "" {
		consumer = "*"
	}
	partiesStyle := lipgloss.NewStyle().Foreground(colorAccent)
	parties := partiesStyle.Render(fmt.Sprintf("%s → %s", ent.Producer, consumer))

	// Status badge.
	sColor := statusColor(ent.Status)
	statusStyle := lipgloss.NewStyle().
		Foreground(sColor).
		Bold(true)
	statusBadge := statusStyle.Render(ent.Status)

	// Interface body as monospace code.
	bodyStyle := lipgloss.NewStyle().
		Foreground(colorMutedLight)
	sig := ent.Signature
	if sig == "" {
		sig = "(no signature)"
	}
	// Truncate long signatures to fit the card width.
	sigLines := strings.Split(sig, "\n")
	var truncatedLines []string
	for _, line := range sigLines {
		if len(line) > innerWidth {
			line = line[:innerWidth-3] + "..."
		}
		truncatedLines = append(truncatedLines, line)
	}
	body := bodyStyle.Render(strings.Join(truncatedLines, "\n"))

	// Compose the card content.
	content := strings.Join([]string{title, parties, statusBadge, body}, "\n")

	// Border style.
	borderColor := colorMuted
	if selected {
		borderColor = colorBlueshift
	}
	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(cardWidth).
		PaddingLeft(1).
		PaddingRight(1)

	return cardStyle.Render(content)
}
