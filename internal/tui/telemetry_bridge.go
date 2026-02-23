// Package tui provides the BubbleTea-based terminal UI.
//
// This file implements TelemetryBridge, a lightweight goroutine that tails
// a JSONL telemetry file and converts selected event kinds into
// MsgScratchpadEntry messages for display in the cockpit scratchpad view.
package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/papapumpkin/quasar/internal/telemetry"
)

// TelemetryBridge tails a JSONL telemetry file and converts interesting
// events into MsgScratchpadEntry messages sent to the TUI program.
// It runs as a background goroutine and stops when the done channel is closed.
type TelemetryBridge struct {
	program  *tea.Program
	path     string
	done     chan struct{}
	stopOnce sync.Once
}

// NewTelemetryBridge creates a bridge that tails the telemetry file at path
// and sends scratchpad messages to the TUI program.
func NewTelemetryBridge(p *tea.Program, path string) *TelemetryBridge {
	return &TelemetryBridge{
		program: p,
		path:    path,
		done:    make(chan struct{}),
	}
}

// Start begins tailing the telemetry file in a background goroutine.
// It reads from the current end of the file, so only new events are surfaced.
func (tb *TelemetryBridge) Start() error {
	f, err := os.Open(tb.path)
	if err != nil {
		return fmt.Errorf("telemetry bridge: open %s: %w", tb.path, err)
	}
	// Seek to end so we only see new events.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		f.Close()
		return fmt.Errorf("telemetry bridge: seek: %w", err)
	}
	go tb.tail(f)
	return nil
}

// Stop signals the tailing goroutine to exit. It is safe to call multiple times.
func (tb *TelemetryBridge) Stop() {
	tb.stopOnce.Do(func() { close(tb.done) })
}

// tail reads lines from the file, polling for new content.
func (tb *TelemetryBridge) tail(f *os.File) {
	defer f.Close()

	scanner := bufio.NewScanner(f)
	const pollInterval = 250 * time.Millisecond

	for {
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var evt telemetry.Event
			if err := json.Unmarshal(line, &evt); err != nil {
				continue // skip malformed lines
			}
			if text := eventToScratchpad(evt); text != "" {
				tb.program.Send(MsgScratchpadEntry{
					Timestamp: evt.Timestamp,
					PhaseID:   evt.TaskID,
					Text:      text,
				})
			}
		}

		// Check for shutdown.
		select {
		case <-tb.done:
			return
		default:
		}

		// Poll for new data.
		select {
		case <-tb.done:
			return
		case <-time.After(pollInterval):
		}

		// Reset scanner to pick up new content appended since last read.
		scanner = bufio.NewScanner(f)
	}
}

// eventToScratchpad converts a telemetry event to a human-readable scratchpad
// string. Returns empty string for events that should not be surfaced.
func eventToScratchpad(evt telemetry.Event) string {
	switch evt.Kind {
	case telemetry.KindDiscoveryPosted:
		return formatEventData(evt, "discovery")
	case telemetry.KindEntanglementPosted:
		return formatEventData(evt, "entanglement")
	case telemetry.KindTaskState:
		return formatTaskState(evt)
	default:
		return ""
	}
}

// formatEventData produces a scratchpad line for discovery/entanglement events.
func formatEventData(evt telemetry.Event, label string) string {
	// Try to extract a detail or name from the data map.
	if m, ok := evt.Data.(map[string]any); ok {
		if detail, ok := m["detail"].(string); ok && detail != "" {
			return fmt.Sprintf("%s: %s", label, detail)
		}
		if name, ok := m["name"].(string); ok && name != "" {
			return fmt.Sprintf("%s: %s", label, name)
		}
	}
	return label + " posted"
}

// formatTaskState produces a scratchpad line for task state transitions.
func formatTaskState(evt telemetry.Event) string {
	m, ok := evt.Data.(map[string]any)
	if !ok {
		return ""
	}
	from, _ := m["from"].(string)
	to, _ := m["to"].(string)
	if from == "" && to == "" {
		return ""
	}
	if from == "" {
		return fmt.Sprintf("→ %s", to)
	}
	return fmt.Sprintf("%s → %s", from, to)
}
