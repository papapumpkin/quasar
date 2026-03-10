package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// activityStreamSize is the maximum number of entries in the activity ring buffer.
const activityStreamSize = 5

// staleActivityThreshold is how long an activity entry can sit without an update
// before being considered stale and rendered with dimmed/elapsed styling.
const staleActivityThreshold = 5 * time.Second

// ActivityEntry is a single timestamped activity event in the ring buffer.
type ActivityEntry struct {
	Text string
	Time time.Time
}

// ActivityStream is a fixed-size ring buffer of recent activity events.
// It stores the last activityStreamSize entries for rolling display.
type ActivityStream struct {
	entries [activityStreamSize]ActivityEntry
	head    int // index of the next write slot
	count   int // number of valid entries (≤ activityStreamSize)
}

// Push adds a new activity entry to the ring buffer.
func (s *ActivityStream) Push(text string) {
	s.entries[s.head] = ActivityEntry{Text: text, Time: time.Now()}
	s.head = (s.head + 1) % activityStreamSize
	if s.count < activityStreamSize {
		s.count++
	}
}

// Latest returns the most recent activity entry.
// Returns a zero ActivityEntry if the buffer is empty.
func (s *ActivityStream) Latest() ActivityEntry {
	if s.count == 0 {
		return ActivityEntry{}
	}
	idx := (s.head - 1 + activityStreamSize) % activityStreamSize
	return s.entries[idx]
}

// Entries returns up to n most recent entries, newest first.
func (s *ActivityStream) Entries(n int) []ActivityEntry {
	if n > s.count {
		n = s.count
	}
	result := make([]ActivityEntry, n)
	for i := 0; i < n; i++ {
		idx := (s.head - 1 - i + activityStreamSize*2) % activityStreamSize
		result[i] = s.entries[idx]
	}
	return result
}

// Len returns the number of entries in the buffer.
func (s *ActivityStream) Len() int {
	return s.count
}

// WorkerCard holds live state for an active quasar (worker) processing a phase.
// Data is populated from MsgPhase* and MsgWorkerActivity messages.
type WorkerCard struct {
	PhaseID    string    // phase being executed
	PhaseTitle string    // human-readable phase title (from nebula spec)
	QuasarID   string    // worker identifier (e.g. "q-1")
	Cycle      int       // current cycle number
	MaxCycles  int       // maximum allowed cycles
	TokensUsed int       // cumulative tokens spent on this phase
	CostUSD    float64   // cumulative cost spent on this phase
	Claims     []string  // file paths currently touched by this quasar
	Activity   string    // human-readable activity: "coding...", "reviewing..."
	AgentRole  string    // "coder" or "reviewer"
	StartedAt  time.Time // when this phase started executing

	// Stream holds the rolling ring buffer of real-time activity events.
	Stream ActivityStream
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

	// Rolling activity stream line — shows latest real-time action from the agent.
	if streamLine := wc.renderStreamLine(innerWidth); streamLine != "" {
		b.WriteString("\n")
		b.WriteString(streamLine)
	}

	// Wrap in a rounded border box.
	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorMuted).
		Width(width-2). // account for border width
		Padding(0, 1)

	return cardStyle.Render(b.String())
}

// renderStreamLine renders the latest activity stream entry. When the entry is
// stale (older than staleActivityThreshold), it appends elapsed time and dims
// the text. Returns an empty string if the stream is empty.
func (wc *WorkerCard) renderStreamLine(maxWidth int) string {
	latest := wc.Stream.Latest()
	if latest.Text == "" {
		return ""
	}

	elapsed := time.Since(latest.Time)
	stale := elapsed >= staleActivityThreshold

	text := latest.Text
	if stale {
		suffix := fmt.Sprintf(" (%s ago)", formatDurationBrief(elapsed))
		available := maxWidth - len(suffix) - 2 // "↳ " prefix
		if available > 0 && len(text) > available {
			text = TruncateWithEllipsis(text, available)
		}
		text = "↳ " + text + suffix
	} else {
		text = "↳ " + text
	}
	text = TruncateWithEllipsis(text, maxWidth)

	style := lipgloss.NewStyle().Foreground(colorMuted)
	if !stale {
		style = style.Foreground(colorMutedLight)
	}
	return style.Render(text)
}

// formatDurationBrief returns a compact human-readable duration string.
func formatDurationBrief(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
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

// quasarNum extracts the numeric suffix from a quasar ID (e.g. "q-12" → 12).
// Returns 0 if the ID doesn't match the expected "q-N" format.
func quasarNum(id string) int {
	after, found := strings.CutPrefix(id, "q-")
	if !found {
		return 0
	}
	n, err := strconv.Atoi(after)
	if err != nil {
		return 0
	}
	return n
}

// sortWorkerCards sorts cards by QuasarID numeric suffix.
// This ensures correct ordering even when IDs reach double digits
// (e.g. "q-9" before "q-10"), unlike a lexicographic comparison.
func sortWorkerCards(cards []*WorkerCard) {
	for i := 1; i < len(cards); i++ {
		for j := i; j > 0 && quasarNum(cards[j].QuasarID) < quasarNum(cards[j-1].QuasarID); j-- {
			cards[j], cards[j-1] = cards[j-1], cards[j]
		}
	}
}
