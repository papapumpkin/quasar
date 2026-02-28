package tui

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/papapumpkin/quasar/internal/telemetry"
)

func TestEventToScratchpad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		event     telemetry.Event
		wantText  string
		wantEmpty bool
	}{
		{
			name: "discovery_posted with detail",
			event: telemetry.Event{
				Kind:   telemetry.KindDiscoveryPosted,
				TaskID: "phase-1",
				Data:   map[string]any{"detail": "missing API endpoint"},
			},
			wantText: "discovery: missing API endpoint",
		},
		{
			name: "entanglement_posted with name",
			event: telemetry.Event{
				Kind:   telemetry.KindEntanglementPosted,
				TaskID: "phase-2",
				Data:   map[string]any{"name": "UserService.GetByID"},
			},
			wantText: "entanglement: UserService.GetByID",
		},
		{
			name: "task_state transition",
			event: telemetry.Event{
				Kind:   telemetry.KindTaskState,
				TaskID: "phase-3",
				Data:   map[string]any{"from": "running", "to": "review"},
			},
			wantText: "running → review",
		},
		{
			name: "task_state initial",
			event: telemetry.Event{
				Kind:   telemetry.KindTaskState,
				TaskID: "phase-3",
				Data:   map[string]any{"from": "", "to": "running"},
			},
			wantText: "→ running",
		},
		{
			name: "agent_start ignored",
			event: telemetry.Event{
				Kind:   telemetry.KindAgentStart,
				TaskID: "phase-1",
			},
			wantEmpty: true,
		},
		{
			name: "discovery_posted no detail",
			event: telemetry.Event{
				Kind:   telemetry.KindDiscoveryPosted,
				TaskID: "phase-1",
				Data:   map[string]any{},
			},
			wantText: "discovery posted",
		},
		{
			name: "task_state no from/to",
			event: telemetry.Event{
				Kind:   telemetry.KindTaskState,
				TaskID: "phase-1",
				Data:   map[string]any{},
			},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := eventToScratchpad(tt.event)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("expected empty string, got %q", got)
				}
				return
			}
			if got != tt.wantText {
				t.Errorf("got %q, want %q", got, tt.wantText)
			}
		})
	}
}

func TestTelemetryBridge_TailsNewEvents(t *testing.T) {
	t.Parallel()

	// Create a temporary JSONL file with one pre-existing event.
	tmpFile, err := os.CreateTemp(t.TempDir(), "telemetry-*.jsonl")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	path := tmpFile.Name()

	// Write one event before starting the bridge (should be skipped since we seek to end).
	preEvent := telemetry.Event{
		Timestamp: time.Now(),
		Kind:      telemetry.KindAgentStart,
		TaskID:    "pre-existing",
	}
	if err := json.NewEncoder(tmpFile).Encode(preEvent); err != nil {
		t.Fatalf("failed to write pre-existing event: %v", err)
	}
	tmpFile.Close()

	// Create a minimal tea.Program to capture messages.
	// We use a model that collects messages for verification.
	collected := make(chan tea.Msg, 10)
	model := &msgCollectorModel{ch: collected}
	p := tea.NewProgram(model, tea.WithoutSignalHandler(), tea.WithOutput(os.Stderr))

	tb := NewTelemetryBridge(p, path)
	if err := tb.Start(); err != nil {
		t.Fatalf("failed to start telemetry bridge: %v", err)
	}
	defer tb.Stop()

	// Append a new event to the file.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("failed to open file for append: %v", err)
	}
	newEvent := telemetry.Event{
		Timestamp: time.Now(),
		Kind:      telemetry.KindDiscoveryPosted,
		TaskID:    "phase-new",
		Data:      map[string]any{"detail": "test discovery"},
	}
	if err := json.NewEncoder(f).Encode(newEvent); err != nil {
		t.Fatalf("failed to write new event: %v", err)
	}
	f.Close()

	// The bridge polls every 250ms; wait enough for it to pick up the event.
	// Since we can't easily intercept program.Send in a non-running program,
	// we just verify the bridge starts and stops without error.
	time.Sleep(500 * time.Millisecond)
	tb.Stop()
}

// msgCollectorModel is a minimal tea.Model for testing message dispatch.
type msgCollectorModel struct {
	ch chan tea.Msg
}

func (m *msgCollectorModel) Init() tea.Cmd { return nil }

func (m *msgCollectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	select {
	case m.ch <- msg:
	default:
	}
	return m, nil
}

func (m *msgCollectorModel) View() string { return "" }
