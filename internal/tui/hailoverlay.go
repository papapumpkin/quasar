package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// Critical hail styles — visually distinct from normal hails.
var (
	// styleHailOverlayCritical uses a double border for critical/blocker hails.
	styleHailOverlayCritical = lipgloss.NewStyle().
					Border(lipgloss.DoubleBorder()).
					BorderForeground(colorDanger).
					Padding(1, 2)

	// styleHailHeaderCritical styles critical hail titles with a red background badge.
	styleHailHeaderCritical = lipgloss.NewStyle().
				Background(colorDanger).
				Foreground(colorBrightWhite).
				Bold(true).
				Padding(0, 1)
)

// HailOverlay renders a red-bordered floating overlay for human decision
// requests. It displays phase context, discovery detail, selectable options,
// and a free-text input line. The overlay is driven by MsgHail messages from
// the fabric and floats centered over the board view with the background dimmed.
type HailOverlay struct {
	PhaseID    string
	QuasarID   string
	Cycle      int
	MaxCycles  int
	Discovery  fabric.Discovery
	Options    []string
	Input      textinput.Model
	ResponseCh chan<- string
	Width      int
	IsCritical bool // true for blocker-kind hails that need red highlighting
}

// NewHailOverlay creates a hail overlay from a MsgHail and optional context.
// The responseCh is used to send the user's response back to the fabric.
func NewHailOverlay(msg MsgHail, responseCh chan<- string) *HailOverlay {
	ti := textinput.New()
	ti.Prompt = "▸ "
	ti.Placeholder = "type a letter or free-text response"
	ti.CharLimit = 256
	ti.Focus()

	// Extract options from the discovery detail by looking for lines
	// that start with "- " (a common pattern in discovery options).
	options := extractOptions(msg.Discovery.Detail)

	// Critical discovery kinds get visually distinct styling.
	isCritical := msg.Discovery.Kind == fabric.DiscoveryMissingDependency ||
		msg.Discovery.Kind == "blocker" ||
		msg.Discovery.Kind == "max_cycles_reached"

	return &HailOverlay{
		PhaseID:    msg.PhaseID,
		Discovery:  msg.Discovery,
		Options:    options,
		Input:      ti,
		ResponseCh: responseCh,
		IsCritical: isCritical,
	}
}

// extractOptions parses option lines from the discovery detail.
// It looks for lines prefixed with "- " and returns them stripped.
func extractOptions(detail string) []string {
	var opts []string
	for _, line := range strings.Split(detail, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			opts = append(opts, strings.TrimPrefix(trimmed, "- "))
		}
	}
	return opts
}

// SetContext sets quasar and cycle context on the overlay.
func (h *HailOverlay) SetContext(quasarID string, cycle, maxCycles int) {
	h.QuasarID = quasarID
	h.Cycle = cycle
	h.MaxCycles = maxCycles
}

// Resolve sends the response and signals completion.
func (h *HailOverlay) Resolve(response string) {
	if h.ResponseCh != nil {
		h.ResponseCh <- response
	}
}

// HandleInput processes the current text input and resolves the overlay.
// If the input is a single letter matching an option index (a-z), it selects
// that option. Otherwise the full text is sent as a free-text response.
func (h *HailOverlay) HandleInput() string {
	val := strings.TrimSpace(h.Input.Value())
	if val == "" {
		return ""
	}

	// Single letter a-z selects an option by index.
	if len(val) == 1 && len(h.Options) > 0 {
		idx := int(val[0] - 'a')
		if idx >= 0 && idx < len(h.Options) {
			return h.Options[idx]
		}
	}

	return val
}

// View renders the hail overlay box content (without centering — the caller
// handles centering and dimming).
func (h HailOverlay) View(width, _ int) string {
	var b strings.Builder

	// Constrain overlay width to terminal width with padding.
	overlayWidth := 60
	if width > 0 && width < overlayWidth+4 {
		overlayWidth = width - 4
	}
	if overlayWidth < 30 {
		overlayWidth = 30
	}

	// Header — critical hails get a more urgent indicator.
	var header string
	if h.IsCritical {
		header = styleHailHeaderCritical.Render("🔴  CRITICAL HAIL")
	} else {
		header = styleHailHeader.Render("⚠  HAIL")
	}
	b.WriteString(header)
	b.WriteString("\n\n")

	// Task context.
	phaseLine := fmt.Sprintf("phase: %s", h.PhaseID)
	b.WriteString(styleHailContext.Render(phaseLine))
	b.WriteString("\n")

	if h.QuasarID != "" {
		quasarLine := fmt.Sprintf("quasar: %s  cycle: %d/%d", h.QuasarID, h.Cycle, h.MaxCycles)
		b.WriteString(styleHailContext.Render(quasarLine))
		b.WriteString("\n")
	}

	kindLine := fmt.Sprintf("kind: %s", h.Discovery.Kind)
	b.WriteString(styleHailKind.Render(kindLine))
	b.WriteString("\n\n")

	// Discovery detail — strip option lines to avoid duplication.
	detail := stripOptionLines(h.Discovery.Detail)
	if detail != "" {
		b.WriteString(styleHailDetail.Render(detail))
		b.WriteString("\n\n")
	}

	// Options.
	for i, opt := range h.Options {
		label := fmt.Sprintf("  %c) %s", 'a'+i, opt)
		b.WriteString(styleHailOption.Render(label))
		b.WriteString("\n")
	}

	if len(h.Options) > 0 {
		b.WriteString("\n")
	}

	// Text input.
	b.WriteString(h.Input.View())

	overlayStyle := styleHailOverlay
	if h.IsCritical {
		overlayStyle = styleHailOverlayCritical
	}
	return overlayStyle.Width(overlayWidth).Render(b.String())
}

// stripOptionLines removes lines starting with "- " from the detail text,
// since those are rendered separately as labeled options.
func stripOptionLines(detail string) string {
	var kept []string
	for _, line := range strings.Split(detail, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") {
			kept = append(kept, line)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}
