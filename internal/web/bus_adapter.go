package web

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/papapumpkin/quasar/internal/bus"
)

// Compile-time interface check.
var _ EventSource = (*BusAdapter)(nil)

// BusAdapter bridges a bus.Bus to the web-local EventSource interface.
// Each Subscribe call creates a new bus subscription and translates
// bus.Event values into web.Event values suitable for SSE streaming.
type BusAdapter struct {
	bus bus.Bus
}

// NewBusAdapter creates an EventSource backed by the given event bus.
func NewBusAdapter(b bus.Bus) *BusAdapter {
	return &BusAdapter{bus: b}
}

// Subscribe implements EventSource. It creates a bus subscription and
// returns a channel of web Events. The cancel function unsubscribes
// from the bus and closes the channel when the forwarding goroutine
// finishes.
func (a *BusAdapter) Subscribe(ctx context.Context) (<-chan Event, func()) {
	sub := a.bus.Subscribe("web-sse", 128)
	ch := make(chan Event, 64)

	go func() {
		defer close(ch)
		for {
			select {
			case ev, ok := <-sub.Events():
				if !ok {
					return
				}
				webEv := busEventToWebEvent(ev)
				select {
				case ch <- webEv:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	cancel := func() {
		sub.Unsubscribe()
	}
	return ch, cancel
}

// busEventToWebEvent converts a bus.Event to a web.Event for SSE streaming.
// The event Type is the bus Kind string; the Data is a JSON object with
// phase, message, and additional context fields depending on the event kind.
// PhaseID is set for internal routing by the SSE filter and PhaseAccumulator.
func busEventToWebEvent(ev bus.Event) Event {
	payload := buildEventPayload(ev)
	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte(fmt.Sprintf(`{"phase":%s,"message":%s}`, jsonString(ev.PhaseID), jsonString(ev.Message)))
	}
	return Event{
		Type:    string(ev.Kind),
		Data:    string(data),
		PhaseID: ev.PhaseID,
	}
}

// eventPayload is the enriched JSON structure sent over SSE and parsed by
// PhaseAccumulator and SSEBridge. All fields are optional; zero values are omitted.
type eventPayload struct {
	Phase      string          `json:"phase"`
	Message    string          `json:"message,omitempty"`
	Role       string          `json:"role,omitempty"`
	Title      string          `json:"title,omitempty"`
	Cycle      int             `json:"cycle,omitempty"`
	MaxCycles  int             `json:"maxCycles,omitempty"`
	CostUSD    float64         `json:"costUSD,omitempty"`
	DurationMs int64           `json:"durationMs,omitempty"`
	Tokens     int             `json:"tokens,omitempty"`
	Count      int             `json:"count,omitempty"`
	Output     string          `json:"output,omitempty"`
	Summary    *summaryPayload `json:"summary,omitempty"`

	// Progress fields — populated for KindNebulaProgress events.
	Completed    int     `json:"completed,omitempty"`
	Total        int     `json:"total,omitempty"`
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
}

// summaryPayload carries structured cycle summary data in event JSON.
type summaryPayload struct {
	Cycle        int     `json:"cycle"`
	Approved     bool    `json:"approved"`
	IssueCount   int     `json:"issueCount"`
	CostUSD      float64 `json:"costUSD"`
	TotalCostUSD float64 `json:"totalCostUSD"`
	DurationMs   int64   `json:"durationMs"`
}

// buildEventPayload constructs an enriched payload from a bus event.
func buildEventPayload(ev bus.Event) eventPayload {
	p := eventPayload{
		Phase:   ev.PhaseID,
		Message: ev.Message,
	}

	// Populate fields based on what the bus event carries.
	if ev.Role != "" {
		p.Role = ev.Role
	}
	if ev.Title != "" {
		p.Title = ev.Title
	}
	if ev.Cycle > 0 {
		p.Cycle = ev.Cycle
	}
	if ev.MaxCycles > 0 {
		p.MaxCycles = ev.MaxCycles
	}
	if ev.CostUSD != 0 {
		p.CostUSD = ev.CostUSD
	}
	if ev.DurationMs != 0 {
		p.DurationMs = ev.DurationMs
	}
	if ev.Tokens != 0 {
		p.Tokens = ev.Tokens
	}
	if ev.Count != 0 {
		p.Count = ev.Count
	}

	// Attach progress payload for nebula progress events.
	if ev.Progress != nil {
		p.Completed = ev.Progress.Completed
		p.Total = ev.Progress.Total
		p.TotalCostUSD = ev.Progress.TotalCostUSD
	}

	// Attach cycle summary when present.
	if ev.CycleSummary != nil {
		p.Summary = &summaryPayload{
			Cycle:        ev.CycleSummary.Cycle,
			Approved:     ev.CycleSummary.Approved,
			IssueCount:   ev.CycleSummary.IssueCount,
			CostUSD:      ev.CycleSummary.CostUSD,
			TotalCostUSD: ev.CycleSummary.TotalCostUSD,
			DurationMs:   ev.CycleSummary.DurationMs,
		}
	}

	return p
}

// jsonString returns s as a JSON-encoded string literal (with surrounding
// quotes). Uses encoding/json.Marshal so all control characters are escaped
// correctly per the JSON spec.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
