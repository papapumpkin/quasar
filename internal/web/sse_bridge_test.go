package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/nebula"
)

// testNebula returns a minimal Nebula with two phases for bridge tests.
func testNebula() *nebula.Nebula {
	return &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "test-nebula"},
			Execution: nebula.Execution{
				MaxReviewCycles: 5,
				MaxBudgetUSD:    10.0,
			},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "phase-1", Title: "First Phase", DependsOn: nil},
			{ID: "phase-2", Title: "Second Phase", DependsOn: []string{"phase-1"}},
		},
	}
}

// testState returns a state with phase-1 done and phase-2 in progress.
func testState() *nebula.State {
	return &nebula.State{
		Phases: map[string]*nebula.PhaseState{
			"phase-1": {Status: nebula.PhaseStatusDone},
			"phase-2": {Status: nebula.PhaseStatusInProgress},
		},
	}
}

func TestSSEBridge_TranslatePhaseEvent(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{NebulaDir: "/tmp/test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.SetNebula(testNebula(), testState())

	bridge := NewSSEBridge(srv)

	data, _ := json.Marshal(phaseEventPayload{Phase: "phase-1"})
	evt := Event{Type: "phase.task.started", Data: string(data)}

	got, err := bridge.TranslateEvent(evt)
	if err != nil {
		t.Fatalf("TranslateEvent: %v", err)
	}

	if got.Type != "phase-update" {
		t.Errorf("Type = %q, want %q", got.Type, "phase-update")
	}
	if !strings.Contains(got.Data, `id="phase-phase-1"`) {
		t.Errorf("expected phase-1 row id, got: %s", got.Data)
	}
	if !strings.Contains(got.Data, `hx-swap-oob="true"`) {
		t.Errorf("expected hx-swap-oob attribute, got: %s", got.Data)
	}
	if !strings.Contains(got.Data, "<tr") {
		t.Errorf("expected <tr> element, got: %s", got.Data)
	}
}

func TestSSEBridge_TranslatePhaseEvent_StatusIcon(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{NebulaDir: "/tmp/test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.SetNebula(testNebula(), testState())

	bridge := NewSSEBridge(srv)

	tests := []struct {
		name     string
		phaseID  string
		wantIcon string
		wantCSS  string
	}{
		{"done phase", "phase-1", "\u2713", "phase-status--done"},
		{"in progress phase", "phase-2", "\u25ce", "phase-status--in_progress"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, _ := json.Marshal(phaseEventPayload{Phase: tt.phaseID})
			evt := Event{Type: "phase.agent.done", Data: string(data)}

			got, err := bridge.TranslateEvent(evt)
			if err != nil {
				t.Fatalf("TranslateEvent: %v", err)
			}
			if !strings.Contains(got.Data, tt.wantIcon) {
				t.Errorf("expected icon %q in: %s", tt.wantIcon, got.Data)
			}
			if !strings.Contains(got.Data, tt.wantCSS) {
				t.Errorf("expected CSS class %q in: %s", tt.wantCSS, got.Data)
			}
		})
	}
}

func TestSSEBridge_TranslateProgressEvent(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{NebulaDir: "/tmp/test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.SetNebula(testNebula(), nil)

	bridge := NewSSEBridge(srv)

	data, _ := json.Marshal(progressEventPayload{
		Completed:    3,
		Total:        10,
		TotalCostUSD: 1.2345,
	})
	evt := Event{Type: "nebula.progress", Data: string(data)}

	got, err := bridge.TranslateEvent(evt)
	if err != nil {
		t.Fatalf("TranslateEvent: %v", err)
	}

	if got.Type != "progress-update" {
		t.Errorf("Type = %q, want %q", got.Type, "progress-update")
	}
	if !strings.Contains(got.Data, `id="status-bar"`) {
		t.Errorf("expected status-bar id, got: %s", got.Data)
	}
	if !strings.Contains(got.Data, `hx-swap-oob="true"`) {
		t.Errorf("expected hx-swap-oob attribute, got: %s", got.Data)
	}
	if !strings.Contains(got.Data, "3/10") {
		t.Errorf("expected progress text 3/10, got: %s", got.Data)
	}
	if !strings.Contains(got.Data, "1.2345") {
		t.Errorf("expected cost 1.2345, got: %s", got.Data)
	}
	if !strings.Contains(got.Data, "30%") {
		t.Errorf("expected 30%% progress bar, got: %s", got.Data)
	}
}

func TestSSEBridge_TranslateNebulaDone(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{NebulaDir: "/tmp/test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	bridge := NewSSEBridge(srv)

	evt := Event{Type: "nebula.done", Data: `{}`}

	got, err := bridge.TranslateEvent(evt)
	if err != nil {
		t.Fatalf("TranslateEvent: %v", err)
	}

	if got.Type != "nebula-done" {
		t.Errorf("Type = %q, want %q", got.Type, "nebula-done")
	}
	if !strings.Contains(got.Data, `id="completion-overlay"`) {
		t.Errorf("expected completion overlay, got: %s", got.Data)
	}
	if !strings.Contains(got.Data, "Nebula Complete") {
		t.Errorf("expected completion text, got: %s", got.Data)
	}
}

func TestSSEBridge_UnknownEventPassthrough(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{NebulaDir: "/tmp/test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	bridge := NewSSEBridge(srv)

	evt := Event{Type: "some.unknown.event", Data: `{"foo":"bar"}`}
	got, err := bridge.TranslateEvent(evt)
	if err != nil {
		t.Fatalf("TranslateEvent: %v", err)
	}

	if got.Type != "some.unknown.event" {
		t.Errorf("Type = %q, want passthrough", got.Type)
	}
	if got.Data != `{"foo":"bar"}` {
		t.Errorf("Data = %q, want passthrough", got.Data)
	}
}

func TestSSEBridge_MalformedEventReturnsError(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{NebulaDir: "/tmp/test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	bridge := NewSSEBridge(srv)

	tests := []struct {
		name string
		evt  Event
	}{
		{"bad phase JSON", Event{Type: "phase.task.started", Data: "not-json"}},
		{"bad progress JSON", Event{Type: "nebula.progress", Data: "not-json"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := bridge.TranslateEvent(tt.evt)
			if err == nil {
				t.Error("expected error for malformed event, got nil")
			}
		})
	}
}

func TestSSEBridge_PhaseNotFound(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{NebulaDir: "/tmp/test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.SetNebula(testNebula(), testState())

	bridge := NewSSEBridge(srv)

	data, _ := json.Marshal(phaseEventPayload{Phase: "nonexistent"})
	evt := Event{Type: "phase.task.started", Data: string(data)}

	_, err = bridge.TranslateEvent(evt)
	if err == nil {
		t.Error("expected error for unknown phase, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention phase ID, got: %v", err)
	}
}

func TestSSEBridge_NoNebulaLoaded(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{NebulaDir: "/tmp/test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// Don't call SetNebula — server has nil nebula.

	bridge := NewSSEBridge(srv)

	data, _ := json.Marshal(phaseEventPayload{Phase: "phase-1"})
	evt := Event{Type: "phase.task.started", Data: string(data)}

	_, err = bridge.TranslateEvent(evt)
	if err == nil {
		t.Error("expected error when no nebula loaded, got nil")
	}
}

func TestSSEBridge_ProgressWithBudget(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{NebulaDir: "/tmp/test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.SetNebula(testNebula(), nil)

	bridge := NewSSEBridge(srv)

	data, _ := json.Marshal(progressEventPayload{
		Completed:    5,
		Total:        10,
		TotalCostUSD: 2.5,
	})
	evt := Event{Type: "nebula.progress", Data: string(data)}

	got, err := bridge.TranslateEvent(evt)
	if err != nil {
		t.Fatalf("TranslateEvent: %v", err)
	}

	// Should include budget since test nebula has MaxBudgetUSD=10.0.
	if !strings.Contains(got.Data, "10.00") {
		t.Errorf("expected budget display, got: %s", got.Data)
	}
	if !strings.Contains(got.Data, "test-nebula") {
		t.Errorf("expected nebula name, got: %s", got.Data)
	}
}

func TestIsPhaseEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bool
	}{
		{"phase.task.started", true},
		{"phase.agent.done", true},
		{"phase.error", true},
		{"nebula.progress", false},
		{"nebula.done", false},
		{"gate.prompt", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			if got := isPhaseEvent(tt.input); got != tt.want {
				t.Errorf("isPhaseEvent(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestEscapeSSEData(t *testing.T) {
	t.Parallel()

	input := "<tr>\n  <td>hello</td>\n</tr>"
	got := escapeSSEData(input)
	if strings.Contains(got, "\n") {
		t.Errorf("expected no newlines, got: %q", got)
	}
	if !strings.Contains(got, "<tr>") {
		t.Errorf("expected HTML content preserved, got: %q", got)
	}
}

func TestSSEBridge_IntegrationSSEStream(t *testing.T) {
	t.Parallel()

	// Set up a source that sends a phase event.
	phaseData, _ := json.Marshal(phaseEventPayload{Phase: "phase-1"})
	progressData, _ := json.Marshal(progressEventPayload{Completed: 1, Total: 2, TotalCostUSD: 0.5})

	source := &mockEventSource{
		events: []Event{
			{Type: "phase.task.started", Data: string(phaseData), PhaseID: "phase-1"},
			{Type: "nebula.progress", Data: string(progressData)},
		},
	}

	srv, err := NewServer(ServerConfig{
		Source:    source,
		NebulaDir: "/tmp/test",
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.SetNebula(testNebula(), testState())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, err := srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		cancel()
		srv.Wait()
	}()

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/events", addr), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()

	// Read SSE stream.
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		n, readErr := resp.Body.Read(buf)
		if readErr != nil {
			done <- ""
			return
		}
		done <- string(buf[:n])
	}()

	select {
	case data := <-done:
		// The bridge should have translated the phase event to phase-update
		// with HTML content.
		if !strings.Contains(data, "event: phase-update") {
			t.Errorf("expected phase-update SSE event, got: %s", data)
		}
		if !strings.Contains(data, "hx-swap-oob") {
			t.Errorf("expected hx-swap-oob in SSE data, got: %s", data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for SSE events")
	}
}
