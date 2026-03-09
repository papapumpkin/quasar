package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/papapumpkin/quasar/internal/nebula"
)

// phaseEventPayload is the JSON structure produced by busEventToWebEvent
// for phase-scoped events. Only Phase is required for row rendering.
type phaseEventPayload struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
}

// progressEventPayload is the JSON structure produced by busEventToWebEvent
// for nebula.progress events. Fields match eventPayload's progress fields.
type progressEventPayload struct {
	Completed    int     `json:"completed"`
	Total        int     `json:"total"`
	TotalCostUSD float64 `json:"total_cost_usd"`
}

// StatusBarData is the template context for the status-bar partial.
type StatusBarData struct {
	NebulaName  string
	Completed   int
	Total       int
	TotalCost   float64
	BudgetUSD   float64
	ProgressPct int
}

// SSEBridge translates EventSource events into HTML-fragment Events
// suitable for HTMX out-of-band swaps. Each translated event carries
// a rendered HTML snippet instead of raw JSON.
type SSEBridge struct {
	server *Server
}

// NewSSEBridge creates an SSEBridge that renders HTML fragments using
// the server's templates and current nebula state.
func NewSSEBridge(s *Server) *SSEBridge {
	return &SSEBridge{server: s}
}

// TranslateEvent converts a raw JSON-data Event into an HTML-fragment
// Event that HTMX can swap into the DOM via hx-swap-oob. Unknown event
// types are passed through unchanged.
func (b *SSEBridge) TranslateEvent(evt Event) (Event, error) {
	switch {
	case isPhaseEvent(evt.Type):
		return b.translatePhaseEvent(evt)
	case evt.Type == "nebula.progress":
		return b.translateProgressEvent(evt)
	case evt.Type == "nebula.done":
		return b.translateNebulaDone(evt)
	default:
		return evt, nil
	}
}

// isPhaseEvent returns true for bus event types scoped to a phase.
func isPhaseEvent(eventType string) bool {
	return strings.HasPrefix(eventType, "phase.")
}

// translatePhaseEvent renders an updated <tr> fragment for the phase
// identified in the event payload.
func (b *SSEBridge) translatePhaseEvent(evt Event) (Event, error) {
	var payload phaseEventPayload
	if err := json.Unmarshal([]byte(evt.Data), &payload); err != nil {
		return Event{}, fmt.Errorf("unmarshal phase event: %w", err)
	}

	html, err := b.renderPhaseRow(payload.Phase)
	if err != nil {
		return Event{}, err
	}
	return Event{Type: "phase-update", Data: html}, nil
}

// translateProgressEvent renders an updated status bar fragment from
// the progress payload.
func (b *SSEBridge) translateProgressEvent(evt Event) (Event, error) {
	var payload progressEventPayload
	if err := json.Unmarshal([]byte(evt.Data), &payload); err != nil {
		return Event{}, fmt.Errorf("unmarshal progress event: %w", err)
	}

	html, err := b.renderStatusBar(payload)
	if err != nil {
		return Event{}, err
	}
	return Event{Type: "progress-update", Data: html}, nil
}

// translateNebulaDone renders a completion banner overlay.
func (b *SSEBridge) translateNebulaDone(_ Event) (Event, error) {
	html := `<div id="completion-overlay" hx-swap-oob="innerHTML" class="completion-banner"><h2>&#x2713; Nebula Complete</h2></div>`
	return Event{Type: "nebula-done", Data: html}, nil
}

// renderPhaseRow builds a PhaseRow from the server's current state
// and renders it using the "phase-row" partial template.
func (b *SSEBridge) renderPhaseRow(phaseID string) (string, error) {
	b.server.mu.RLock()
	neb := b.server.nebula
	state := b.server.state
	b.server.mu.RUnlock()

	if neb == nil {
		return "", fmt.Errorf("no nebula loaded")
	}

	// Find the phase spec.
	var spec *nebula.PhaseSpec
	for i := range neb.Phases {
		if neb.Phases[i].ID == phaseID {
			spec = &neb.Phases[i]
			break
		}
	}
	if spec == nil {
		return "", fmt.Errorf("phase %q not found", phaseID)
	}

	if state == nil {
		state = &nebula.State{Phases: make(map[string]*nebula.PhaseState)}
	}

	// Determine current status.
	status := nebula.PhaseStatusPending
	if ps := state.Phases[phaseID]; ps != nil {
		status = ps.Status
	}

	// Compute max review cycles.
	maxCycles := spec.MaxReviewCycles
	if maxCycles == 0 {
		maxCycles = neb.Manifest.Execution.MaxReviewCycles
	}
	if maxCycles == 0 {
		maxCycles = 5
	}

	// Build DAG for wave and blocked-by info.
	dg, waves := buildDAGAndWaves(neb.Phases)
	waveForPhase := mapPhasesToWaves(waves)
	blockedBy := computeBlockedBy(phaseID, spec.DependsOn, state, dg)

	row := PhaseRow{
		ID:         spec.ID,
		Title:      spec.Title,
		Status:     statusString(status),
		StatusIcon: statusIcon(status),
		CostUSD:    fmt.Sprintf("%.4f", 0.0),
		Cycles:     fmt.Sprintf("0/%d", maxCycles),
		Wave:       waveForPhase[spec.ID],
		BlockedBy:  blockedBy,
		DependsOn:  spec.DependsOn,
	}

	return b.renderTemplate("phase-row", row)
}

// renderStatusBar renders the "status-bar" partial from progress data.
func (b *SSEBridge) renderStatusBar(p progressEventPayload) (string, error) {
	pct := 0
	if p.Total > 0 {
		pct = (p.Completed * 100) / p.Total
	}

	b.server.mu.RLock()
	neb := b.server.nebula
	b.server.mu.RUnlock()

	budgetUSD := 0.0
	nebulaName := ""
	if neb != nil {
		budgetUSD = neb.Manifest.Execution.MaxBudgetUSD
		nebulaName = neb.Manifest.Nebula.Name
	}

	data := StatusBarData{
		NebulaName:  nebulaName,
		Completed:   p.Completed,
		Total:       p.Total,
		TotalCost:   p.TotalCostUSD,
		BudgetUSD:   budgetUSD,
		ProgressPct: pct,
	}

	return b.renderTemplate("status-bar", data)
}

// renderTemplate executes a named template into a string.
func (b *SSEBridge) renderTemplate(name string, data any) (string, error) {
	var buf bytes.Buffer
	if err := b.server.templates.ExecuteTemplate(&buf, name, data); err != nil {
		return "", fmt.Errorf("render %s: %w", name, err)
	}
	return strings.TrimSpace(buf.String()), nil
}

// escapeSSEData replaces newlines in HTML fragments so they can be
// sent as a single SSE data: line. HTMX's SSE extension handles
// multi-line data fields, but single-line is safer.
func escapeSSEData(s string) string {
	return strings.ReplaceAll(s, "\n", "")
}
